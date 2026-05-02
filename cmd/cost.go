package cmd

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"github.com/emcniece/s3-summary/internal/billing"
)

var (
	costDays  int
	costStart string
	costEnd   string
)

var costCmd = &cobra.Command{
	Use:   "cost",
	Short: "Show actual billed S3 costs from Cost Explorer, grouped by usage type",
	Long: `Calls AWS Cost Explorer (ce:GetCostAndUsage) to retrieve S3 spend over
a date range, grouped by USAGE_TYPE. This decomposes a bill into storage,
requests, retrieval, data transfer, etc. — answering "where is my S3 money
actually going?"

Cost Explorer must be enabled on the account (one-click opt-in, free) and
the IAM principal needs ce:GetCostAndUsage. Cost Explorer is hosted only
in us-east-1; this command forces that region regardless of your default.

Date range defaults to the last 30 days. Override with --days, or pass an
explicit --start/--end pair (YYYY-MM-DD). End is exclusive.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		start, end, err := resolveRange(costDays, costStart, costEnd)
		if err != nil {
			return err
		}
		ctx := context.Background()
		fmt.Fprintf(os.Stderr, "Querying Cost Explorer for S3 spend %s through %s (exclusive)...\n",
			start.Format("2006-01-02"), end.Format("2006-01-02"))
		report, err := billing.S3CostByUsageType(ctx, start, end)
		if err != nil {
			return err
		}
		return renderCost(os.Stdout, report, flagFormat)
	},
}

func resolveRange(days int, startStr, endStr string) (time.Time, time.Time, error) {
	if startStr != "" || endStr != "" {
		if startStr == "" || endStr == "" {
			return time.Time{}, time.Time{}, fmt.Errorf("both --start and --end must be provided together")
		}
		s, err := time.Parse("2006-01-02", startStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --start: %w", err)
		}
		e, err := time.Parse("2006-01-02", endStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --end: %w", err)
		}
		if !e.After(s) {
			return time.Time{}, time.Time{}, fmt.Errorf("--end must be after --start")
		}
		return s, e, nil
	}
	if days <= 0 {
		days = 30
	}
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -days)
	return start, end, nil
}

func renderCost(w *os.File, r *billing.Report, format string) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	case "csv":
		cw := csv.NewWriter(w)
		defer cw.Flush()
		if err := cw.Write([]string{"usage_type", "amount_usd"}); err != nil {
			return err
		}
		for _, it := range r.Items {
			if err := cw.Write([]string{it.UsageType, fmt.Sprintf("%.4f", it.AmountUSD)}); err != nil {
				return err
			}
		}
		return cw.Write([]string{"TOTAL", fmt.Sprintf("%.4f", r.TotalUSD)})
	}

	t := table.NewWriter()
	t.SetOutputMirror(w)
	t.SetTitle(fmt.Sprintf("S3 Costs by Usage Type (%s through %s, end exclusive)",
		r.Start.Format("2006-01-02"), r.End.Format("2006-01-02")))
	t.AppendHeader(table.Row{"Usage Type", "Cost (USD)", "% of Total"})
	for _, it := range r.Items {
		share := 0.0
		if r.TotalUSD > 0 {
			share = 100 * it.AmountUSD / r.TotalUSD
		}
		t.AppendRow(table.Row{it.UsageType, fmt.Sprintf("$%.4f", it.AmountUSD), fmt.Sprintf("%.1f%%", share)})
	}
	t.AppendFooter(table.Row{"TOTAL", fmt.Sprintf("$%.4f", r.TotalUSD), ""})
	t.Render()
	fmt.Fprintln(w, "\nReading: storage shows up as TimedStorage-ByteHrs (or *-IntelligentTiering, *-GlacierByteHrs, etc).")
	fmt.Fprintln(w, "Requests: Requests-Tier1 (PUT/COPY/POST/LIST), Requests-Tier2 (GET/SELECT), Requests-Tier3 (Glacier).")
	fmt.Fprintln(w, "Surprises: EarlyDelete-* (Glacier early deletion), Retrieval-* (Glacier restore), DataTransfer-Out-Bytes.")
	return nil
}

func init() {
	costCmd.Flags().IntVar(&costDays, "days", 30, "lookback window in days (ignored if --start/--end set)")
	costCmd.Flags().StringVar(&costStart, "start", "", "explicit start date YYYY-MM-DD (inclusive)")
	costCmd.Flags().StringVar(&costEnd, "end", "", "explicit end date YYYY-MM-DD (exclusive)")
	rootCmd.AddCommand(costCmd)
}
