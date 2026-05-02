package analyzer

import (
	"time"
)

// AccessSignal indicates which signal was used to evaluate object age.
type AccessSignal string

const (
	SignalLastAccess   AccessSignal = "LastAccessTime"
	SignalLastModified AccessSignal = "LastModified"
)

// ClassStats aggregates objects within a single storage class.
type ClassStats struct {
	StorageClass string
	ObjectCount  int64
	TotalBytes   int64
	// AgeBuckets counts objects by their age relative to the scan time, in days.
	// Buckets: 0-30, 31-90, 91-180, 181-365, 365+
	AgeBuckets [5]int64
	// BytesByAgeBucket parallels AgeBuckets and tracks total bytes per bucket.
	BytesByAgeBucket [5]int64
	// SmallObjectCount is objects under 128KB (cannot benefit from IA).
	SmallObjectCount int64
	// SmallObjectBytes is total bytes of objects under 128KB.
	SmallObjectBytes int64
}

// BucketSummary aggregates the result of analyzing a single bucket.
type BucketSummary struct {
	Name         string
	Region       string
	ScannedAt    time.Time
	AccessSignal AccessSignal
	Source       string // "live" or "inventory"
	Classes      map[string]*ClassStats
	Errors       []string
}

// AccountSummary is the aggregate across all buckets in the run.
type AccountSummary struct {
	GeneratedAt time.Time
	Buckets     []*BucketSummary
}

// SmallObjectThresholdBytes is the size below which IA storage charges a
// minimum-object-size penalty (128 KB).
const SmallObjectThresholdBytes = 128 * 1024

// AgeBucketIndex returns the AgeBuckets index for an object of the given age in days.
func AgeBucketIndex(ageDays int) int {
	switch {
	case ageDays <= 30:
		return 0
	case ageDays <= 90:
		return 1
	case ageDays <= 180:
		return 2
	case ageDays <= 365:
		return 3
	default:
		return 4
	}
}

// AgeBucketLabels are human-readable labels for AgeBuckets indices.
var AgeBucketLabels = [5]string{"0-30d", "31-90d", "91-180d", "181-365d", "365d+"}

// Record adds a single object's stats to the summary.
func (s *BucketSummary) Record(class string, sizeBytes int64, refTime time.Time, objAge time.Time) {
	if s.Classes == nil {
		s.Classes = make(map[string]*ClassStats)
	}
	if class == "" {
		class = "STANDARD"
	}
	cs, ok := s.Classes[class]
	if !ok {
		cs = &ClassStats{StorageClass: class}
		s.Classes[class] = cs
	}
	cs.ObjectCount++
	cs.TotalBytes += sizeBytes

	ageDays := max(int(refTime.Sub(objAge).Hours()/24), 0)
	idx := AgeBucketIndex(ageDays)
	cs.AgeBuckets[idx]++
	cs.BytesByAgeBucket[idx] += sizeBytes

	if sizeBytes < SmallObjectThresholdBytes {
		cs.SmallObjectCount++
		cs.SmallObjectBytes += sizeBytes
	}
}
