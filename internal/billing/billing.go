// Package billing wraps AWS Cost Explorer to fetch actual billed S3 costs
// broken down by usage type. Cost Explorer is a us-east-1-only API and may
// not be enabled on every account.
package billing

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

// LineItem is one row of the breakdown.
type LineItem struct {
	UsageType string  `json:"usage_type"`
	AmountUSD float64 `json:"amount_usd"`
	UnitsRaw  string  `json:"units"`
}

// Report is the full S3 cost breakdown for a date range.
type Report struct {
	Start    time.Time  `json:"start"`
	End      time.Time  `json:"end"`
	TotalUSD float64    `json:"total_usd"`
	Items    []LineItem `json:"items"`
}

// S3CostByUsageType queries Cost Explorer for S3 spend in [start, end) grouped
// by USAGE_TYPE. End is exclusive (Cost Explorer convention).
func S3CostByUsageType(ctx context.Context, start, end time.Time) (*Report, error) {
	// Cost Explorer is hosted only in us-east-1.
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	client := costexplorer.NewFromConfig(cfg)

	in := &costexplorer.GetCostAndUsageInput{
		TimePeriod: &cetypes.DateInterval{
			Start: aws.String(start.Format("2006-01-02")),
			End:   aws.String(end.Format("2006-01-02")),
		},
		Granularity: cetypes.GranularityMonthly,
		Metrics:     []string{"UnblendedCost"},
		GroupBy: []cetypes.GroupDefinition{
			{Type: cetypes.GroupDefinitionTypeDimension, Key: aws.String("USAGE_TYPE")},
		},
		Filter: &cetypes.Expression{
			Dimensions: &cetypes.DimensionValues{
				Key:    cetypes.DimensionService,
				Values: []string{"Amazon Simple Storage Service"},
			},
		},
	}

	report := &Report{Start: start, End: end}
	totals := make(map[string]float64)

	for {
		out, err := client.GetCostAndUsage(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("get cost and usage: %w", err)
		}
		for _, period := range out.ResultsByTime {
			for _, group := range period.Groups {
				if len(group.Keys) == 0 {
					continue
				}
				usage := group.Keys[0]
				m, ok := group.Metrics["UnblendedCost"]
				if !ok || m.Amount == nil {
					continue
				}
				amount := parseAmount(*m.Amount)
				totals[usage] += amount
			}
		}
		if out.NextPageToken == nil || *out.NextPageToken == "" {
			break
		}
		in.NextPageToken = out.NextPageToken
	}

	for usage, amt := range totals {
		report.Items = append(report.Items, LineItem{UsageType: usage, AmountUSD: amt})
		report.TotalUSD += amt
	}
	sort.Slice(report.Items, func(i, j int) bool {
		return report.Items[i].AmountUSD > report.Items[j].AmountUSD
	})
	return report, nil
}

func parseAmount(s string) float64 {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	if err != nil {
		return 0
	}
	return f
}
