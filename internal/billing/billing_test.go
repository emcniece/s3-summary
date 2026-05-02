package billing

import (
	"math"
	"testing"
)

func TestParseAmount(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0", 0},
		{"1.5", 1.5},
		{"19.875", 19.875},
		{"0.0001234", 0.0001234},
		{"", 0},
		{"abc", 0},
	}
	for _, tc := range cases {
		got := parseAmount(tc.in)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("parseAmount(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
