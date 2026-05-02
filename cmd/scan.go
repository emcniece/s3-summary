package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/emcniece/s3-summary/internal/analyzer"
	"github.com/emcniece/s3-summary/internal/awsclient"
	"github.com/emcniece/s3-summary/internal/report"
	"github.com/emcniece/s3-summary/internal/scanner"
)

var (
	scanConcurrency int
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Live-scan all buckets via ListObjectsV2",
	Long: `Walks every bucket in the caller's account using ListObjectsV2 to
aggregate object counts, total bytes, storage classes, and age distributions.

Note: ListObjectsV2 is paginated and metered. For very large buckets (>1M
objects) prefer 'inventory' against an existing S3 Inventory report.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		client, err := awsclient.New(ctx)
		if err != nil {
			return err
		}

		fmt.Fprintln(os.Stderr, "Scanning buckets...")
		summary, err := scanner.Scan(ctx, client, scanner.Options{
			Concurrency: scanConcurrency,
			Progress: func(name string, _ *analyzer.BucketSummary, _ error) {
				fmt.Fprintf(os.Stderr, "  scanned %s\n", name)
			},
		})
		if err != nil {
			return err
		}
		return report.Render(os.Stdout, summary, report.Format(flagFormat))
	},
}

func init() {
	scanCmd.Flags().IntVar(&scanConcurrency, "concurrency", 4, "number of buckets to scan in parallel")
	rootCmd.AddCommand(scanCmd)
}
