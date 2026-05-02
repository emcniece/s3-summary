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

Typical usage types to watch for:

| Usage type pattern              | What it means                                |
| ------------------------------- | -------------------------------------------- |
| `TimedStorage-ByteHrs`          | STANDARD storage                             |
| `TimedStorage-*-IA-ByteHrs`     | IA storage                                   |
| `TimedStorage-GlacierByteHrs`   | Glacier storage                              |
| `Requests-Tier1`                | PUT, COPY, POST, LIST                        |
| `Requests-Tier2`                | GET, SELECT                                  |
| `Retrieval-*`                   | Glacier restore (often a surprise)           |
| `EarlyDelete-*`                 | Deleting Glacier objects before min duration |
| `DataTransfer-Out-Bytes`        | Outbound data to internet                    |

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
