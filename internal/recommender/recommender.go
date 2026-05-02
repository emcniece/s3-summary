package recommender

import (
	"fmt"

	"github.com/emcniece/s3-summary/internal/analyzer"
	"github.com/emcniece/s3-summary/internal/pricing"
)

// Recommendation describes a suggested storage-class transition for a slice of
// objects within a bucket.
type Recommendation struct {
	Bucket          string
	CurrentClass    string
	SuggestedClass  string
	AgeBucket       string
	ObjectCount     int64
	Bytes           int64
	Reason          string
	EstMonthlySaved float64 // USD per month (rough, see PriceSnapshot)
}

// Build returns the per-bucket list of recommendations.
//
// Default rules (AWS published guidance, conservative):
//   - Objects < 128KB: leave in STANDARD (IA imposes a 128KB minimum-billable size).
//   - STANDARD, age > 30d: STANDARD_IA
//   - STANDARD or STANDARD_IA, age > 90d: GLACIER_IR
//   - age > 180d: GLACIER (Flexible Retrieval)
//   - age > 365d: DEEP_ARCHIVE
//
// We only emit a recommendation when the suggested class differs from the current.
func Build(summary *analyzer.BucketSummary) []Recommendation {
	if summary == nil {
		return nil
	}
	var recs []Recommendation
	for class, stats := range summary.Classes {
		for i, count := range stats.AgeBuckets {
			if count == 0 {
				continue
			}
			bytes := stats.BytesByAgeBucket[i]
			suggested := suggestForAge(class, i)
			if suggested == "" || suggested == class {
				continue
			}

			// Subtract small-object share when transitioning into IA, since
			// objects under 128KB don't benefit. This is approximate — we
			// don't know how small objects distribute across age buckets.
			eligibleBytes := bytes
			eligibleCount := count
			if suggested == "STANDARD_IA" || suggested == "ONEZONE_IA" {
				if stats.ObjectCount > 0 {
					smallShare := float64(stats.SmallObjectCount) / float64(stats.ObjectCount)
					eligibleBytes = int64(float64(bytes) * (1 - smallShare))
					eligibleCount = int64(float64(count) * (1 - smallShare))
				}
			}
			if eligibleCount <= 0 {
				continue
			}

			saved := estimateMonthlySavings(class, suggested, eligibleBytes)
			recs = append(recs, Recommendation{
				Bucket:          summary.Name,
				CurrentClass:    class,
				SuggestedClass:  suggested,
				AgeBucket:       analyzer.AgeBucketLabels[i],
				ObjectCount:     eligibleCount,
				Bytes:           eligibleBytes,
				Reason:          fmt.Sprintf("Objects in %s aged %s have not been modified recently", class, analyzer.AgeBucketLabels[i]),
				EstMonthlySaved: saved,
			})
		}
	}
	return recs
}

// suggestForAge returns the recommended class for an object currently in
// `current` whose age falls in AgeBuckets index `ageIdx`.
func suggestForAge(current string, ageIdx int) string {
	// Already-cold storage classes don't need to be moved colder by age alone.
	switch current {
	case "GLACIER", "DEEP_ARCHIVE":
		return ""
	}

	switch ageIdx {
	case 0: // 0-30d
		return current
	case 1: // 31-90d
		if current == "STANDARD" || current == "REDUCED_REDUNDANCY" {
			return "STANDARD_IA"
		}
		return current
	case 2: // 91-180d
		if current == "STANDARD" || current == "STANDARD_IA" || current == "REDUCED_REDUNDANCY" {
			return "GLACIER_IR"
		}
		return current
	case 3: // 181-365d
		return "GLACIER"
	case 4: // 365d+
		return "DEEP_ARCHIVE"
	}
	return current
}

// estimateMonthlySavings returns rough $/month savings for moving `bytes` from
// `from` to `to`. Returns 0 if either price is unknown or the move costs more.
func estimateMonthlySavings(from, to string, bytes int64) float64 {
	delta := pricing.MonthlyStorageCost(from, bytes) - pricing.MonthlyStorageCost(to, bytes)
	if delta < 0 {
		return 0
	}
	return delta
}
