package crosscheck

import (
	"math"
	"testing"
)

func alignedDay(d int, values ...SourceValue) AlignedDay {
	return AlignedDay{Date: testDay(1, d), Values: values}
}

func TestFindDeviations(t *testing.T) {
	aligned := []AlignedDay{
		// Day 1: 10 vs 11 -> 10% relative difference, equal to threshold: no deviation.
		alignedDay(1,
			SourceValue{Source: "tefas", ClosePrice: 10},
			SourceValue{Source: "yahoo", ClosePrice: 11},
		),
		// Day 2: 100 vs 120 -> 20% > 10%: deviation.
		alignedDay(2,
			SourceValue{Source: "tefas", ClosePrice: 100},
			SourceValue{Source: "yahoo", ClosePrice: 120},
		),
		// Day 3: three sources, one pair deviates (tefas 10 vs yahoo 13 = 30%).
		alignedDay(3,
			SourceValue{Source: "isyatirim", ClosePrice: 10.5},
			SourceValue{Source: "tefas", ClosePrice: 10},
			SourceValue{Source: "yahoo", ClosePrice: 13},
		),
		// Day 4: zero value cannot be compared relatively.
		alignedDay(4,
			SourceValue{Source: "tefas", ClosePrice: 0},
			SourceValue{Source: "yahoo", ClosePrice: 99},
		),
	}

	devs := findDeviations(aligned, 0.10)

	// Day 2 pair + day 3 pairs: isyatirim/tefas (5%, no), isyatirim/yahoo
	// (~23.8%, yes), tefas/yahoo (30%, yes).
	if len(devs) != 3 {
		t.Fatalf("expected 3 deviations, got %d: %+v", len(devs), devs)
	}

	d0 := devs[0]
	if !d0.Date.Equal(testDay(1, 2)) || d0.SourceA != "tefas" || d0.SourceB != "yahoo" {
		t.Fatalf("deviation 0 identity mismatch: %+v", d0)
	}
	if math.Abs(d0.RelativeDifference-0.20) > 1e-12 {
		t.Fatalf("expected 0.20 relative difference, got %v", d0.RelativeDifference)
	}

	// Day 3: pairs must be emitted in source-sorted order.
	if devs[1].SourceA != "isyatirim" || devs[1].SourceB != "yahoo" {
		t.Fatalf("deviation 1 identity mismatch: %+v", devs[1])
	}
	if devs[2].SourceA != "tefas" || devs[2].SourceB != "yahoo" {
		t.Fatalf("deviation 2 identity mismatch: %+v", devs[2])
	}
}

func TestFindDeviations_Disabled(t *testing.T) {
	aligned := []AlignedDay{
		alignedDay(1,
			SourceValue{Source: "a", ClosePrice: 1},
			SourceValue{Source: "b", ClosePrice: 1000},
		),
	}
	if devs := findDeviations(aligned, 0); devs != nil {
		t.Fatalf("zero threshold must disable deviations, got %+v", devs)
	}
	if devs := findDeviations(aligned, -1); devs != nil {
		t.Fatalf("negative threshold must disable deviations, got %+v", devs)
	}
}

func TestFindDeviations_ThresholdBoundary(t *testing.T) {
	// Base 100, other 105 => exactly 5%: must not be flagged at 5%.
	aligned := []AlignedDay{
		alignedDay(1,
			SourceValue{Source: "a", ClosePrice: 100},
			SourceValue{Source: "b", ClosePrice: 105},
		),
	}
	if devs := findDeviations(aligned, 0.05); len(devs) != 0 {
		t.Fatalf("difference equal to threshold must not be a deviation: %+v", devs)
	}
	if devs := findDeviations(aligned, 0.0499); len(devs) != 1 {
		t.Fatalf("difference above smaller threshold must be a deviation")
	}
}
