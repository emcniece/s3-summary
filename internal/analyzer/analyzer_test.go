package analyzer

import (
	"testing"
	"time"
)

func TestAgeBucketIndexBoundaries(t *testing.T) {
	cases := []struct {
		ageDays int
		want    int
	}{
		{0, 0},
		{30, 0},
		{31, 1},
		{90, 1},
		{91, 2},
		{180, 2},
		{181, 3},
		{365, 3},
		{366, 4},
		{10_000, 4},
	}
	for _, tc := range cases {
		if got := AgeBucketIndex(tc.ageDays); got != tc.want {
			t.Errorf("AgeBucketIndex(%d) = %d, want %d", tc.ageDays, got, tc.want)
		}
	}
}

func TestRecordIncrementsCorrectBucket(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	s := &BucketSummary{Name: "demo"}

	// 200 days old => index 3 (181-365d).
	s.Record("STANDARD", 5*1024*1024, now, now.AddDate(0, 0, -200))

	cs := s.Classes["STANDARD"]
	if cs == nil {
		t.Fatal("expected STANDARD class entry")
	}
	if cs.ObjectCount != 1 {
		t.Errorf("ObjectCount = %d, want 1", cs.ObjectCount)
	}
	if cs.TotalBytes != 5*1024*1024 {
		t.Errorf("TotalBytes = %d, want %d", cs.TotalBytes, 5*1024*1024)
	}
	if cs.AgeBuckets[3] != 1 {
		t.Errorf("AgeBuckets[3] = %d, want 1", cs.AgeBuckets[3])
	}
	if cs.BytesByAgeBucket[3] != 5*1024*1024 {
		t.Errorf("BytesByAgeBucket[3] = %d, want %d", cs.BytesByAgeBucket[3], 5*1024*1024)
	}
}

func TestRecordEmptyClassDefaultsToStandard(t *testing.T) {
	now := time.Now().UTC()
	s := &BucketSummary{Name: "demo"}
	s.Record("", 1024, now, now)
	if _, ok := s.Classes["STANDARD"]; !ok {
		t.Fatal("expected empty class to default to STANDARD")
	}
}

func TestRecordTracksSmallObjects(t *testing.T) {
	now := time.Now().UTC()
	s := &BucketSummary{Name: "demo"}

	// One small (100 KiB) and one big (1 MiB).
	s.Record("STANDARD", 100*1024, now, now)
	s.Record("STANDARD", 1024*1024, now, now)

	cs := s.Classes["STANDARD"]
	if cs.SmallObjectCount != 1 {
		t.Errorf("SmallObjectCount = %d, want 1", cs.SmallObjectCount)
	}
	if cs.SmallObjectBytes != 100*1024 {
		t.Errorf("SmallObjectBytes = %d, want %d", cs.SmallObjectBytes, 100*1024)
	}
}

func TestRecordHandlesFutureModifiedTime(t *testing.T) {
	// If LastModified is somehow in the future (clock skew), age should clamp to 0
	// rather than producing a negative bucket index.
	now := time.Now().UTC()
	s := &BucketSummary{Name: "demo"}
	s.Record("STANDARD", 1024, now, now.Add(24*time.Hour))
	cs := s.Classes["STANDARD"]
	if cs.AgeBuckets[0] != 1 {
		t.Errorf("future-dated object should land in bucket 0, got AgeBuckets=%v", cs.AgeBuckets)
	}
}
