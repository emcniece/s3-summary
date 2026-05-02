package scanner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/emcniece/s3-summary/internal/analyzer"
	"github.com/emcniece/s3-summary/internal/awsclient"
)

// Options configures a scan run.
type Options struct {
	// Concurrency limits how many buckets are scanned in parallel.
	Concurrency int
	// Progress is called once per bucket as it completes (best-effort).
	Progress func(bucket string, summary *analyzer.BucketSummary, err error)
}

// Scan walks every bucket in the account using ListObjectsV2 and returns an
// aggregated AccountSummary. Buckets the caller cannot access are reported in
// each BucketSummary's Errors and skipped, not fatal.
func Scan(ctx context.Context, c *awsclient.Client, opts Options) (*analyzer.AccountSummary, error) {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	buckets, err := c.ListAllBuckets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list buckets: %w", err)
	}

	now := time.Now().UTC()
	results := make([]*analyzer.BucketSummary, len(buckets))
	sem := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup

	for i, b := range buckets {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			summary := scanBucket(ctx, c, aws.ToString(b.Name), now)
			results[i] = summary
			if opts.Progress != nil {
				opts.Progress(aws.ToString(b.Name), summary, nil)
			}
		}()
	}
	wg.Wait()

	return &analyzer.AccountSummary{
		GeneratedAt: now,
		Buckets:     results,
	}, nil
}

func scanBucket(ctx context.Context, c *awsclient.Client, bucket string, now time.Time) *analyzer.BucketSummary {
	summary := &analyzer.BucketSummary{
		Name:         bucket,
		ScannedAt:    now,
		AccessSignal: analyzer.SignalLastModified,
		Source:       "live",
	}

	region, err := c.BucketRegion(ctx, bucket)
	if err != nil {
		summary.Errors = append(summary.Errors, fmt.Sprintf("get region: %v", classify(err)))
		return summary
	}
	summary.Region = region

	client := c.ForRegion(region)
	p := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	})
	for p.HasMorePages() {
		out, err := p.NextPage(ctx)
		if err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("list objects: %v", classify(err)))
			return summary
		}
		for _, obj := range out.Contents {
			class := string(obj.StorageClass)
			var size int64
			if obj.Size != nil {
				size = *obj.Size
			}
			ageRef := now
			if obj.LastModified != nil {
				ageRef = *obj.LastModified
			}
			summary.Record(class, size, now, ageRef)
		}
	}
	return summary
}

// classify reduces noisy AWS errors to a compact string for summary reporting.
func classify(err error) string {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		return ae.ErrorCode()
	}
	return err.Error()
}
