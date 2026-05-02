package awsclient

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Client wraps the AWS config and a cache of region-specific S3 clients.
// S3 list/get operations on a bucket must use the bucket's home region;
// using the wrong region triggers redirects (or PermanentRedirect errors).
type Client struct {
	cfg     aws.Config
	root    *s3.Client
	mu      sync.Mutex
	regions map[string]*s3.Client
}

// New loads the default AWS config and returns a Client.
func New(ctx context.Context) (*Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return &Client{
		cfg:     cfg,
		root:    s3.NewFromConfig(cfg),
		regions: make(map[string]*s3.Client),
	}, nil
}

// Root returns the S3 client built from the user's default region. Use it for
// account-wide operations like ListBuckets.
func (c *Client) Root() *s3.Client {
	return c.root
}

// ForRegion returns an S3 client configured for the given region. Cached.
func (c *Client) ForRegion(region string) *s3.Client {
	if region == "" {
		return c.root
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.regions[region]; ok {
		return existing
	}
	client := s3.NewFromConfig(c.cfg, func(o *s3.Options) {
		o.Region = region
	})
	c.regions[region] = client
	return client
}

// BucketRegion resolves a bucket's home region. The empty string from
// GetBucketLocation means us-east-1, per the S3 API quirk.
func (c *Client) BucketRegion(ctx context.Context, bucket string) (string, error) {
	out, err := c.root.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return "", err
	}
	if out.LocationConstraint == "" {
		return "us-east-1", nil
	}
	return string(out.LocationConstraint), nil
}

// ListAllBuckets returns every bucket in the caller's account.
func (c *Client) ListAllBuckets(ctx context.Context) ([]types.Bucket, error) {
	var buckets []types.Bucket
	p := s3.NewListBucketsPaginator(c.root, &s3.ListBucketsInput{})
	for p.HasMorePages() {
		out, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		buckets = append(buckets, out.Buckets...)
	}
	return buckets, nil
}
