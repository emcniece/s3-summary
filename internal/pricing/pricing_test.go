package pricing

import (
	"math"
	"testing"
)

func TestMonthlyStorageCostKnownClass(t *testing.T) {
	// 1 GiB in STANDARD at $0.023/GB-month should be $0.023.
	got := MonthlyStorageCost("STANDARD", bytesPerGB)
	if math.Abs(got-0.023) > 1e-9 {
		t.Fatalf("STANDARD 1GiB: got %v, want 0.023", got)
	}
}

func TestMonthlyStorageCostScalesLinearly(t *testing.T) {
	// 100 GiB in DEEP_ARCHIVE = 100 * 0.00099 = 0.099.
	got := MonthlyStorageCost("DEEP_ARCHIVE", 100*bytesPerGB)
	want := 0.099
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("DEEP_ARCHIVE 100GiB: got %v, want %v", got, want)
	}
}

func TestMonthlyStorageCostUnknownClass(t *testing.T) {
	if got := MonthlyStorageCost("MADE_UP_CLASS", bytesPerGB); got != 0 {
		t.Fatalf("unknown class: got %v, want 0", got)
	}
}

func TestMonthlyStorageCostNonPositiveBytes(t *testing.T) {
	if got := MonthlyStorageCost("STANDARD", 0); got != 0 {
		t.Fatalf("zero bytes: got %v, want 0", got)
	}
	if got := MonthlyStorageCost("STANDARD", -1024); got != 0 {
		t.Fatalf("negative bytes: got %v, want 0", got)
	}
}
