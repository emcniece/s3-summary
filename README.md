# s3-summary

A CLI for figuring out where your AWS S3 bill is going.

`s3-summary` scans every bucket in your account, tallies object counts, sizes,
storage classes, and ages, then recommends storage-class transitions based on
AWS's published cost breakpoints. It supports two data sources: a live
`ListObjectsV2` scan, and reading an existing S3 Inventory report.

## Install

Requires Go 1.22+.

```bash
go install github.com/emcniece/s3-summary@latest
```

Or build from source:

```bash
git clone https://github.com/emcniece/s3-summary
cd s3-summary
go build -o s3-summary .
```

### Docker

Multi-arch images (`linux/amd64` and `linux/arm64`) are published to GitHub
Container Registry on every release:

```bash
docker pull ghcr.io/emcniece/s3-summary:latest
# or pin a version:
docker pull ghcr.io/emcniece/s3-summary:0.1.0
```

Pass AWS credentials in via environment variables:

```bash
docker run --rm \
  -e AWS_ACCESS_KEY_ID="$AWS_ACCESS_KEY_ID" \
  -e AWS_SECRET_ACCESS_KEY="$AWS_SECRET_ACCESS_KEY" \
  -e AWS_SESSION_TOKEN="$AWS_SESSION_TOKEN" \
  -e AWS_REGION="${AWS_REGION:-us-east-1}" \
  ghcr.io/emcniece/s3-summary:latest scan
```

`AWS_SESSION_TOKEN` is only needed for STS / temporary credentials (SSO,
assumed roles); leave it unset for plain IAM user keys. `AWS_REGION` sets
the default region the SDK uses for service calls; bucket-specific regions
are still resolved per bucket.

If you'd rather use your existing `~/.aws/credentials` and SSO config,
mount the directory in. The image runs as the `nonroot` user (UID 65532)
with `HOME=/home/nonroot`:

```bash
docker run --rm \
  -v "$HOME/.aws:/home/nonroot/.aws:ro" \
  -e AWS_PROFILE="${AWS_PROFILE:-default}" \
  ghcr.io/emcniece/s3-summary:latest scan
```

The image is built from `gcr.io/distroless/static-debian12:nonroot` — about
2 MB of base layers, no shell, no package manager.

## Authentication

Uses the standard AWS credential chain — environment variables,
`~/.aws/credentials`, IAM roles, SSO. Whatever `aws sts get-caller-identity`
works with will work here.

The IAM principal needs:

- `s3:ListAllMyBuckets`
- `s3:GetBucketLocation`
- `s3:ListBucket` on every bucket you want scanned
- `s3:GetObject` on the inventory destination bucket (for `inventory` mode)
- `ce:GetCostAndUsage` (for `cost` mode)

For `cost` mode you also need Cost Explorer enabled on the account — a free,
one-click opt-in in the AWS console. Cost Explorer is hosted only in
`us-east-1`; the tool forces that region for the cost call regardless of
your default.

## Usage

### Live scan

Walks every bucket in the account, paginating through `ListObjectsV2`.

```bash
s3-summary scan
s3-summary scan --concurrency 8
s3-summary scan --format json > summary.json
s3-summary scan --format csv > summary.csv
```

For very large buckets (millions of objects) `ListObjectsV2` is slow and racks
up request charges. Use `inventory` instead.

### S3 Inventory

Reads an existing S3 Inventory manifest and aggregates from the report files,
which avoids paying for `ListObjectsV2` on huge buckets.

```bash
s3-summary inventory s3://my-inventory-bucket/source-bucket/daily/2026-04-30T00-00Z/manifest.json
```

You can pass multiple manifest URIs to aggregate across buckets in one run.

CSV manifests are supported (gzip-aware). Parquet and ORC are not yet
implemented.

### Cost breakdown

Pulls actual billed S3 costs from Cost Explorer for the last 30 days
(by default) and groups them by usage type — storage, requests, retrieval,
data transfer, early-delete fees, etc. This is the only mode that returns
real billed dollars rather than estimates.

```bash
s3-summary cost
s3-summary cost --days 90
s3-summary cost --start 2026-04-01 --end 2026-05-01
s3-summary cost --format json
```

#### Reading usage type codes

Most usage types are prefixed with a region code: `USE1` = us-east-1,
`USE2` = us-east-2, `USW1` = us-west-1, `USW2` = us-west-2,
`EUW1` = eu-west-1, etc. Usage in us-east-1 often appears with no prefix
at all. So `USW2-TimedStorage-ByteHrs` means **STANDARD storage in
us-west-2**, billed in byte-hours.

Storage charges (the "rent" for keeping bytes around):

| Code suffix                  | Storage class                                 |
| ---------------------------- | --------------------------------------------- |
| `TimedStorage-ByteHrs`       | STANDARD                                      |
| `TimedStorage-SIA-ByteHrs`   | Standard-IA                                   |
| `TimedStorage-ZIA-ByteHrs`   | One Zone-IA                                   |
| `TimedStorage-GIR-ByteHrs`   | Glacier Instant Retrieval                     |
| `TimedStorage-GlacierByteHrs`| Glacier Flexible Retrieval                    |
| `TimedStorage-GDA-ByteHrs`   | Glacier Deep Archive                          |
| `TimedStorage-INT-FA-ByteHrs`| Intelligent-Tiering, Frequent Access tier     |
| `TimedStorage-INT-IA-ByteHrs`| Intelligent-Tiering, Infrequent Access tier   |

Other charge categories worth knowing:

| Code pattern              | What it means                                    |
| ------------------------- | ------------------------------------------------ |
| `Requests-Tier1`          | PUT, COPY, POST, LIST                            |
| `Requests-Tier2`          | GET, SELECT                                      |
| `Requests-Tier3`          | Glacier restore requests                         |
| `Retrieval-*`             | Glacier restore byte charges (often a surprise)  |
| `EarlyDelete-*`           | Deleting Glacier objects before minimum duration |
| `DataTransfer-Out-Bytes`  | Outbound data to the internet                    |
| `DataTransfer-Regional-*` | Cross-region replication / cross-region GETs     |

## How recommendations work

The recommender uses AWS's default age breakpoints:

| Current class           | Age          | Suggested            |
| ----------------------- | ------------ | -------------------- |
| `STANDARD`              | 31–90 days   | `STANDARD_IA`        |
| `STANDARD` / IA         | 91–180 days  | `GLACIER_IR`         |
| any warm class          | 181–365 days | `GLACIER`            |
| any warm class          | 365+ days    | `DEEP_ARCHIVE`       |

Objects under 128 KiB are excluded from IA suggestions because IA bills a
minimum object size that wipes out the savings.

Estimated monthly savings use us-east-1 list prices and exclude transition
request charges. They are coarse — use them to prioritize, not to forecast.

## Limitations

- **Age signal is `LastModified`, not `LastAccessTime`.** Real access dates
  only exist when S3 Storage Class Analysis or Intelligent-Tiering is enabled
  on the bucket. The output footer says which signal was used.
- **CSV inventory only.** Parquet and ORC manifests return a clear error.
- **Pricing is us-east-1 list prices.** Estimates in other regions will skew.
- **`ListObjectsV2` does not return access times.** Live scans always use
  `LastModified`; if you need true access-based recommendations, configure
  S3 Inventory with the `LastAccessTime` field and use `inventory` mode.
