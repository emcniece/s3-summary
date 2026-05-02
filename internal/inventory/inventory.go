package inventory

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/emcniece/s3-summary/internal/analyzer"
	"github.com/emcniece/s3-summary/internal/awsclient"
)

// Manifest is the subset of an S3 Inventory manifest.json we consume.
// Reference: https://docs.aws.amazon.com/AmazonS3/latest/userguide/storage-inventory.html#storage-inventory-location
type Manifest struct {
	SourceBucket      string         `json:"sourceBucket"`
	DestinationBucket string         `json:"destinationBucket"`
	Version           string         `json:"version"`
	CreationTimestamp string         `json:"creationTimestamp"`
	FileFormat        string         `json:"fileFormat"`
	FileSchema        string         `json:"fileSchema"`
	Files             []ManifestFile `json:"files"`
}

type ManifestFile struct {
	Key         string `json:"key"`
	Size        int64  `json:"size"`
	MD5Checksum string `json:"MD5checksum"`
}

// Read parses a manifest at s3://destBucket/prefix/manifest.json and returns
// the bucket summary derived from its data files. CSV (optionally gzip) is
// supported; Parquet/ORC manifests return an error explaining the limitation.
func Read(ctx context.Context, c *awsclient.Client, manifestURI string) (*analyzer.BucketSummary, error) {
	bucket, key, err := parseS3URI(manifestURI)
	if err != nil {
		return nil, err
	}

	region, err := c.BucketRegion(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("resolve manifest bucket region: %w", err)
	}
	client := c.ForRegion(region)

	manifest, err := fetchManifest(ctx, client, bucket, key)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(manifest.FileFormat, "CSV") {
		return nil, fmt.Errorf("unsupported inventory format %q (only CSV is supported in this build)", manifest.FileFormat)
	}

	cols := strings.Split(manifest.FileSchema, ",")
	for i := range cols {
		cols[i] = strings.TrimSpace(cols[i])
	}
	idx := buildColumnIndex(cols)
	if idx.bucket < 0 || idx.size < 0 || idx.lastModified < 0 {
		return nil, fmt.Errorf("inventory schema missing required columns (Bucket, Size, LastModifiedDate); got %q", manifest.FileSchema)
	}

	now := time.Now().UTC()
	summary := &analyzer.BucketSummary{
		Name:         manifest.SourceBucket,
		Region:       region,
		ScannedAt:    now,
		AccessSignal: analyzer.SignalLastModified,
		Source:       "inventory",
	}

	for _, f := range manifest.Files {
		if err := readDataFile(ctx, client, manifest.DestinationBucket, f.Key, idx, summary, now); err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("%s: %v", f.Key, err))
		}
	}
	return summary, nil
}

func fetchManifest(ctx context.Context, client *s3.Client, bucket, key string) (*Manifest, error) {
	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get manifest: %w", err)
	}
	defer out.Body.Close()
	var m Manifest
	if err := json.NewDecoder(out.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &m, nil
}

type columnIndex struct {
	bucket       int
	key          int
	size         int
	lastModified int
	storageClass int
}

func buildColumnIndex(cols []string) columnIndex {
	idx := columnIndex{bucket: -1, key: -1, size: -1, lastModified: -1, storageClass: -1}
	for i, c := range cols {
		switch c {
		case "Bucket":
			idx.bucket = i
		case "Key":
			idx.key = i
		case "Size":
			idx.size = i
		case "LastModifiedDate":
			idx.lastModified = i
		case "StorageClass":
			idx.storageClass = i
		}
	}
	return idx
}

func readDataFile(ctx context.Context, client *s3.Client, bucket, key string, idx columnIndex, summary *analyzer.BucketSummary, now time.Time) error {
	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("get data file: %w", err)
	}
	defer out.Body.Close()

	var reader io.Reader = out.Body
	if strings.HasSuffix(key, ".gz") {
		gz, err := gzip.NewReader(out.Body)
		if err != nil {
			return fmt.Errorf("gzip: %w", err)
		}
		defer gz.Close()
		reader = gz
	}

	r := csv.NewReader(reader)
	r.FieldsPerRecord = -1
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("csv: %w", err)
		}
		recordRow(row, idx, summary, now)
	}
	return nil
}

func recordRow(row []string, idx columnIndex, summary *analyzer.BucketSummary, now time.Time) {
	if idx.size >= len(row) || idx.lastModified >= len(row) {
		return
	}
	size, err := strconv.ParseInt(row[idx.size], 10, 64)
	if err != nil {
		return
	}
	// Inventory's LastModifiedDate is ISO-8601 UTC.
	lastMod, err := time.Parse(time.RFC3339, row[idx.lastModified])
	if err != nil {
		// Some inventory exports use "2006-01-02T15:04:05.000Z" with milliseconds.
		lastMod, err = time.Parse("2006-01-02T15:04:05.000Z", row[idx.lastModified])
		if err != nil {
			return
		}
	}
	class := ""
	if idx.storageClass >= 0 && idx.storageClass < len(row) {
		class = row[idx.storageClass]
	}
	summary.Record(class, size, now, lastMod)
}

// parseS3URI accepts s3://bucket/key/path forms.
func parseS3URI(raw string) (bucket, key string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("parse uri: %w", err)
	}
	if u.Scheme != "s3" {
		return "", "", fmt.Errorf("expected s3:// uri, got %q", raw)
	}
	if u.Host == "" || u.Path == "" {
		return "", "", fmt.Errorf("missing bucket or key in %q", raw)
	}
	return u.Host, strings.TrimPrefix(u.Path, "/"), nil
}
