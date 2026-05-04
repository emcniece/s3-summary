package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	flagFormat string
)

var rootCmd = &cobra.Command{
	Use:   "s3-summary",
	Short: "Analyze AWS S3 storage costs across all buckets",
	Long: `s3-summary scans every bucket in your AWS account, tallies object
counts, sizes, storage classes, and ages, then recommends storage-class
transitions based on AWS's published cost breakpoints.`,
}

// SetVersionInfo populates the metadata shown by --version. Called from
// main.go with values injected at build time by goreleaser; falls back to
// dev defaults for plain `go build`.
func SetVersionInfo(version, commit, date string) {
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagFormat, "format", "f", "table", "output format: table | json | csv")
	rootCmd.SetVersionTemplate("s3-summary {{ .Version }}\n")
}
