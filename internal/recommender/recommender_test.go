package recommender

import (
	"testing"
	"time"

	"github.com/emcniece/s3-summary/internal/analyzer"
)

func TestSuggestForAge(t *testing.T) {
	cases := []struct {
		name    string
		current string
		ageIdx  int
		want    string
	}{
		{"standard fresh stays put", "STANDARD", 0, "STANDARD"},
		{"standard 31-90d -> IA", "STANDARD", 1, "STANDARD_IA"},
		{"standard 91-180d -> Glacier IR", "STANDARD", 2, "GLACIER_IR"},
		{"standard 181-365d -> Glacier", "STANDARD", 3, "GLACIER"},
		{"standard >365d -> Deep Archive", "STANDARD", 4, "DEEP_ARCHIVE"},
		{"glacier never moves on age alone", "GLACIER", 4, ""},
		{"deep archive never moves", "DEEP_ARCHIVE", 4, ""},
		{"IA at 91-180d -> Glacier IR", "STANDARD_IA", 2, "GLACIER_IR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := suggestForAge(tc.current, tc.ageIdx)
			if got != tc.want {
				t.Fatalf("suggestForAge(%s, %d) = %q, want %q", tc.current, tc.ageIdx, got, tc.want)
			}
		})
	}
}

func TestBuildEmitsRecommendations(t *testing.T) {
	now := time.Now().UTC()
	summary := &analyzer.BucketSummary{
		Name:    "demo",
		Region:  "us-east-1",
		Source:  "live",
		Classes: map[string]*analyzer.ClassStats{},
	}
	// 100 objects, all 10 MiB, all 200 days old, all STANDARD.
	for i := 0; i < 100; i++ {
		summary.Record("STANDARD", 10*1024*1024, now, now.AddDate(0, 0, -200))
	}
	recs := Build(summary)
	if len(recs) == 0 {
		t.Fatal("expected at least one recommendation, got none")
	}
	r := recs[0]
	if r.SuggestedClass != "GLACIER" {
		t.Fatalf("expected GLACIER recommendation for 200d STANDARD, got %s", r.SuggestedClass)
	}
	if r.EstMonthlySaved <= 0 {
		t.Fatalf("expected positive savings estimate, got %f", r.EstMonthlySaved)
	}
}

func TestBuildSkipsAlreadyOptimalAge(t *testing.T) {
	now := time.Now().UTC()
	summary := &analyzer.BucketSummary{
		Name:    "demo",
		Source:  "live",
		Classes: map[string]*analyzer.ClassStats{},
	}
	// All objects are <30d old in STANDARD — no recommendations should fire.
	for i := 0; i < 5; i++ {
		summary.Record("STANDARD", 1024*1024, now, now.AddDate(0, 0, -5))
	}
	if recs := Build(summary); len(recs) != 0 {
		t.Fatalf("expected 0 recommendations for fresh STANDARD objects, got %d", len(recs))
	}
}
