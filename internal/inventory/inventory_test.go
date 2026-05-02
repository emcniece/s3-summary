package inventory

import (
	"testing"
	"time"

	"github.com/emcniece/s3-summary/internal/analyzer"
)

func TestParseS3URI(t *testing.T) {
	cases := []struct {
		raw        string
		wantBucket string
		wantKey    string
		wantErr    bool
	}{
		{"s3://my-bucket/path/to/manifest.json", "my-bucket", "path/to/manifest.json", false},
		{"s3://my-bucket/manifest.json", "my-bucket", "manifest.json", false},
		{"http://my-bucket/manifest.json", "", "", true},
		{"s3://my-bucket", "", "", true},
		{"s3:///key", "", "", true},
		{"not a url at all !@#$", "", "", true},
	}
	for _, tc := range cases {
		gotBucket, gotKey, err := parseS3URI(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseS3URI(%q): expected error, got bucket=%q key=%q", tc.raw, gotBucket, gotKey)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseS3URI(%q): unexpected error: %v", tc.raw, err)
			continue
		}
		if gotBucket != tc.wantBucket || gotKey != tc.wantKey {
			t.Errorf("parseS3URI(%q) = (%q, %q), want (%q, %q)",
				tc.raw, gotBucket, gotKey, tc.wantBucket, tc.wantKey)
		}
	}
}

func TestBuildColumnIndex(t *testing.T) {
	idx := buildColumnIndex([]string{"Bucket", "Key", "Size", "LastModifiedDate", "StorageClass"})
	if idx.bucket != 0 || idx.key != 1 || idx.size != 2 || idx.lastModified != 3 || idx.storageClass != 4 {
		t.Fatalf("unexpected index: %+v", idx)
	}
}

func TestBuildColumnIndexMissingOptional(t *testing.T) {
	idx := buildColumnIndex([]string{"Bucket", "Key", "Size", "LastModifiedDate"})
	if idx.storageClass != -1 {
		t.Fatalf("expected storageClass=-1 when absent, got %d", idx.storageClass)
	}
}

func TestRecordRowParsesRFC3339(t *testing.T) {
	idx := columnIndex{bucket: 0, key: 1, size: 2, lastModified: 3, storageClass: 4}
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	row := []string{"my-bucket", "obj1", "1048576", "2026-04-01T00:00:00Z", "STANDARD"}
	s := &analyzer.BucketSummary{Name: "my-bucket"}
	recordRow(row, idx, s, now)

	cs := s.Classes["STANDARD"]
	if cs == nil || cs.ObjectCount != 1 {
		t.Fatalf("expected one STANDARD object, got %+v", cs)
	}
	// 30 days old => bucket 0.
	if cs.AgeBuckets[0] != 1 {
		t.Errorf("expected age bucket 0, got AgeBuckets=%v", cs.AgeBuckets)
	}
}

func TestRecordRowParsesMillisecondFormat(t *testing.T) {
	idx := columnIndex{bucket: 0, key: 1, size: 2, lastModified: 3, storageClass: 4}
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	// "2006-01-02T15:04:05.000Z" fallback format.
	row := []string{"my-bucket", "obj1", "2048", "2026-04-01T00:00:00.000Z", "STANDARD_IA"}
	s := &analyzer.BucketSummary{Name: "my-bucket"}
	recordRow(row, idx, s, now)
	if s.Classes["STANDARD_IA"] == nil {
		t.Fatal("expected millisecond-format date to parse and produce STANDARD_IA entry")
	}
}

func TestRecordRowSkipsMalformedRows(t *testing.T) {
	idx := columnIndex{bucket: 0, key: 1, size: 2, lastModified: 3, storageClass: 4}
	now := time.Now().UTC()
	s := &analyzer.BucketSummary{Name: "demo"}

	// Bad size.
	recordRow([]string{"b", "k", "not-a-number", "2026-04-01T00:00:00Z", "STANDARD"}, idx, s, now)
	// Bad date.
	recordRow([]string{"b", "k", "1024", "yesterday", "STANDARD"}, idx, s, now)
	// Too few columns.
	recordRow([]string{"b", "k"}, idx, s, now)

	if len(s.Classes) != 0 {
		t.Fatalf("expected no records from malformed rows, got %d classes", len(s.Classes))
	}
}
