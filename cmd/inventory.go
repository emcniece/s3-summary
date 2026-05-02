package cmd

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/emcniece/s3-summary/internal/analyzer"
	"github.com/emcniece/s3-summary/internal/awsclient"
	"github.com/emcniece/s3-summary/internal/inventory"
	"github.com/emcniece/s3-summary/internal/report"
)

var inventoryCmd = &cobra.Command{
	Use:   "inventory <s3://manifest-uri> [<s3://manifest-uri>...]",
	Short: "Aggregate one or more S3 Inventory manifests",
	Long: `Reads S3 Inventory manifest.json files (CSV format) and produces the
same summary report as the 'scan' command, but without paying for ListObjectsV2.

Inventory must be configured on the source bucket and have produced at least
one report. Pass the s3:// URI of the manifest.json file.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		client, err := awsclient.New(ctx)
		if err != nil {
			return err
		}
		account := &analyzer.AccountSummary{GeneratedAt: time.Now().UTC()}
		for _, uri := range args {
			summary, err := inventory.Read(ctx, client, uri)
			if err != nil {
				summary = &analyzer.BucketSummary{
					Name:    uri,
					Source:  "inventory",
					Errors:  []string{err.Error()},
					Classes: map[string]*analyzer.ClassStats{},
				}
			}
			account.Buckets = append(account.Buckets, summary)
		}
		return report.Render(os.Stdout, account, report.Format(flagFormat))
	},
}

func init() {
	rootCmd.AddCommand(inventoryCmd)
}
