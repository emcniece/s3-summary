// Package pricing holds rough per-GB-month list prices used to estimate S3
// storage costs. These are coarse — they're for ordering and rough estimates
// only. Real billing depends on region, requests, retrieval, minimum object
// size penalties, and any negotiated discounts. Use the `cost` subcommand
// against Cost Explorer for actual billed amounts.
package pricing

// USDPerGBMonth holds us-east-1 list prices in USD per GB-month.
var USDPerGBMonth = map[string]float64{
	"STANDARD":            0.023,
	"INTELLIGENT_TIERING": 0.023,
	"STANDARD_IA":         0.0125,
	"ONEZONE_IA":          0.01,
	"GLACIER_IR":          0.004,
	"GLACIER":             0.0036,
	"DEEP_ARCHIVE":        0.00099,
	"REDUCED_REDUNDANCY":  0.024,
}

const bytesPerGB = 1024 * 1024 * 1024

// MonthlyStorageCost returns the rough $/month for `bytes` stored in `class`.
// Returns 0 if the class is unknown.
func MonthlyStorageCost(class string, bytes int64) float64 {
	rate, ok := USDPerGBMonth[class]
	if !ok || bytes <= 0 {
		return 0
	}
	return rate * float64(bytes) / float64(bytesPerGB)
}
