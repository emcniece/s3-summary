package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/jedib0t/go-pretty/v6/table"

	"github.com/emcniece/s3-summary/internal/analyzer"
	"github.com/emcniece/s3-summary/internal/pricing"
	"github.com/emcniece/s3-summary/internal/recommender"
)

// Format is the user-selected output format.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatCSV   Format = "csv"
)

// Render writes the summary and recommendations in the requested format.
func Render(w io.Writer, summary *analyzer.AccountSummary, format Format) error {
	if w == nil {
		w = os.Stdout
	}
	switch format {
	case FormatJSON:
		return renderJSON(w, summary)
	case FormatCSV:
		return renderCSV(w, summary)
	case FormatTable, "":
		return renderTable(w, summary)
	}
	return fmt.Errorf("unknown format %q", format)
}

func renderTable(w io.Writer, summary *analyzer.AccountSummary) error {
	buckets := append([]*analyzer.BucketSummary(nil), summary.Buckets...)
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Name < buckets[j].Name })

	overview := table.NewWriter()
	overview.SetOutputMirror(w)
	overview.SetTitle("S3 Summary by Bucket")
	overview.AppendHeader(table.Row{"Bucket", "Region", "Class", "Objects", "Total Size", "Small <128KB", "Est $/mo", "Source", "Errors"})

	accountTotal := 0.0
	for _, b := range buckets {
		if len(b.Classes) == 0 {
			overview.AppendRow(table.Row{b.Name, b.Region, "-", 0, "0 B", "0", "$0.00", b.Source, errSummary(b)})
			continue
		}
		bucketTotal := 0.0
		classes := sortedClasses(b)
		for i, class := range classes {
			cs := b.Classes[class]
			classCost := pricing.MonthlyStorageCost(class, cs.TotalBytes)
			bucketTotal += classCost
			name := b.Name
			region := b.Region
			source := b.Source
			errs := errSummary(b)
			if i > 0 {
				name, region, source, errs = "", "", "", ""
			}
			overview.AppendRow(table.Row{
				name, region, class,
				cs.ObjectCount,
				humanBytes(cs.TotalBytes),
				cs.SmallObjectCount,
				fmt.Sprintf("$%.2f", classCost),
				source, errs,
			})
		}
		overview.AppendRow(table.Row{"", "", "  bucket subtotal", "", "", "", fmt.Sprintf("$%.2f", bucketTotal), "", ""})
		overview.AppendSeparator()
		accountTotal += bucketTotal
	}
	overview.AppendFooter(table.Row{"", "", "", "", "", "ACCOUNT TOTAL", fmt.Sprintf("$%.2f", accountTotal), "", ""})
	overview.Render()
	fmt.Fprintln(w)

	// Cost ranking by bucket
	rankTable := table.NewWriter()
	rankTable.SetOutputMirror(w)
	rankTable.SetTitle("Buckets Ranked by Estimated Monthly Storage Cost")
	rankTable.AppendHeader(table.Row{"Rank", "Bucket", "Region", "Total Size", "Est $/mo", "% of Account"})
	type rankRow struct {
		bucket *analyzer.BucketSummary
		cost   float64
		bytes  int64
	}
	var ranked []rankRow
	for _, b := range buckets {
		var totalBytes int64
		var cost float64
		for class, cs := range b.Classes {
			totalBytes += cs.TotalBytes
			cost += pricing.MonthlyStorageCost(class, cs.TotalBytes)
		}
		ranked = append(ranked, rankRow{bucket: b, cost: cost, bytes: totalBytes})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].cost > ranked[j].cost })
	for i, r := range ranked {
		share := 0.0
		if accountTotal > 0 {
			share = 100 * r.cost / accountTotal
		}
		rankTable.AppendRow(table.Row{
			i + 1, r.bucket.Name, r.bucket.Region,
			humanBytes(r.bytes),
			fmt.Sprintf("$%.2f", r.cost),
			fmt.Sprintf("%.1f%%", share),
		})
	}
	rankTable.Render()
	fmt.Fprintln(w)

	// Recommendations
	recTable := table.NewWriter()
	recTable.SetOutputMirror(w)
	recTable.SetTitle("Recommendations")
	recTable.AppendHeader(table.Row{"Bucket", "From", "To", "Age", "Objects", "Bytes", "Est. $/mo Saved"})
	totalSaved := 0.0
	for _, b := range buckets {
		for _, r := range recommender.Build(b) {
			recTable.AppendRow(table.Row{
				r.Bucket, r.CurrentClass, r.SuggestedClass, r.AgeBucket,
				r.ObjectCount, humanBytes(r.Bytes),
				fmt.Sprintf("$%.2f", r.EstMonthlySaved),
			})
			totalSaved += r.EstMonthlySaved
		}
	}
	recTable.AppendFooter(table.Row{"", "", "", "", "", "TOTAL", fmt.Sprintf("$%.2f", totalSaved)})
	recTable.Render()

	fmt.Fprintf(w, "\nGenerated: %s\nNote: Costs are rough us-east-1 list-price estimates for storage only — they exclude requests, data transfer, and retrieval. Run `s3-summary cost` for actual billed amounts from Cost Explorer.\n", summary.GeneratedAt.Format("2006-01-02 15:04:05 UTC"))
	return nil
}

func renderJSON(w io.Writer, summary *analyzer.AccountSummary) error {
	type classOut struct {
		StorageClass string  `json:"storage_class"`
		Objects      int64   `json:"objects"`
		Bytes        int64   `json:"bytes"`
		EstMonthly   float64 `json:"est_monthly_cost_usd"`
	}
	type bucketOut struct {
		Name            string     `json:"name"`
		Region          string     `json:"region"`
		Source          string     `json:"source"`
		Classes         []classOut `json:"classes"`
		EstMonthlyTotal float64    `json:"est_monthly_cost_usd"`
		Errors          []string   `json:"errors,omitempty"`
	}
	type out struct {
		GeneratedAt        string                       `json:"generated_at"`
		AccountTotalUSD    float64                      `json:"account_total_est_monthly_cost_usd"`
		Buckets            []bucketOut                  `json:"buckets"`
		Recommendations    []recommender.Recommendation `json:"recommendations"`
	}

	var allRecs []recommender.Recommendation
	bucketOuts := make([]bucketOut, 0, len(summary.Buckets))
	accountTotal := 0.0
	for _, b := range summary.Buckets {
		bo := bucketOut{Name: b.Name, Region: b.Region, Source: b.Source, Errors: b.Errors}
		for _, class := range sortedClasses(b) {
			cs := b.Classes[class]
			cost := pricing.MonthlyStorageCost(class, cs.TotalBytes)
			bo.Classes = append(bo.Classes, classOut{
				StorageClass: class,
				Objects:      cs.ObjectCount,
				Bytes:        cs.TotalBytes,
				EstMonthly:   cost,
			})
			bo.EstMonthlyTotal += cost
		}
		accountTotal += bo.EstMonthlyTotal
		bucketOuts = append(bucketOuts, bo)
		allRecs = append(allRecs, recommender.Build(b)...)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out{
		GeneratedAt:     summary.GeneratedAt.Format("2006-01-02T15:04:05Z"),
		AccountTotalUSD: accountTotal,
		Buckets:         bucketOuts,
		Recommendations: allRecs,
	})
}

func renderCSV(w io.Writer, summary *analyzer.AccountSummary) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()
	if err := cw.Write([]string{"bucket", "region", "storage_class", "objects", "bytes", "small_objects_under_128kb", "est_monthly_usd", "source", "errors"}); err != nil {
		return err
	}
	for _, b := range summary.Buckets {
		errs := errSummary(b)
		if len(b.Classes) == 0 {
			if err := cw.Write([]string{b.Name, b.Region, "", "0", "0", "0", "0", b.Source, errs}); err != nil {
				return err
			}
			continue
		}
		for _, class := range sortedClasses(b) {
			cs := b.Classes[class]
			cost := pricing.MonthlyStorageCost(class, cs.TotalBytes)
			if err := cw.Write([]string{
				b.Name, b.Region, class,
				fmt.Sprint(cs.ObjectCount),
				fmt.Sprint(cs.TotalBytes),
				fmt.Sprint(cs.SmallObjectCount),
				fmt.Sprintf("%.4f", cost),
				b.Source, errs,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortedClasses(b *analyzer.BucketSummary) []string {
	classes := make([]string, 0, len(b.Classes))
	for c := range b.Classes {
		classes = append(classes, c)
	}
	sort.Strings(classes)
	return classes
}

func errSummary(b *analyzer.BucketSummary) string {
	if len(b.Errors) == 0 {
		return ""
	}
	if len(b.Errors) == 1 {
		return b.Errors[0]
	}
	return fmt.Sprintf("%s (+%d more)", b.Errors[0], len(b.Errors)-1)
}

const (
	kib = 1024
	mib = 1024 * kib
	gib = 1024 * mib
	tib = 1024 * gib
)

func humanBytes(n int64) string {
	switch {
	case n >= tib:
		return fmt.Sprintf("%.2f TiB", float64(n)/float64(tib))
	case n >= gib:
		return fmt.Sprintf("%.2f GiB", float64(n)/float64(gib))
	case n >= mib:
		return fmt.Sprintf("%.2f MiB", float64(n)/float64(mib))
	case n >= kib:
		return fmt.Sprintf("%.2f KiB", float64(n)/float64(kib))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
