package crosscheck

import (
	"testing"
)

func TestDedupeSource(t *testing.T) {
	series := SourceSeries{
		Source: "tefas",
		Points: []Point{
			{Date: testDay(1, 1), ClosePrice: 10},
			// Same day, deliberately shuffled: result must not depend on order.
			{Date: testDay(1, 2), ClosePrice: 30},
			{Date: testDay(1, 2), ClosePrice: 10},
			{Date: testDay(1, 2), ClosePrice: 20},
			// Even count: mean of the two middle values.
			{Date: testDay(1, 3), ClosePrice: 40},
			{Date: testDay(1, 3), ClosePrice: 100},
		},
	}

	deduped, hitDays, duplicates := dedupeSource(series)

	if hitDays != 3 {
		t.Fatalf("expected 3 hit days (duplicates count once), got %d", hitDays)
	}
	if duplicates != 3 {
		t.Fatalf("expected 3 collapsed observations, got %d", duplicates)
	}
	if got := deduped[testDay(1, 1)]; got != 10 {
		t.Errorf("day 1: expected 10, got %v", got)
	}
	if got := deduped[testDay(1, 2)]; got != 20 {
		t.Errorf("day 2: expected median 20, got %v", got)
	}
	if got := deduped[testDay(1, 3)]; got != 70 {
		t.Errorf("day 3: expected middle mean 70, got %v", got)
	}
}

func TestDedupeSource_OrderIndependent(t *testing.T) {
	a := SourceSeries{Source: "x", Points: []Point{
		{Date: testDay(2, 1), ClosePrice: 1},
		{Date: testDay(2, 1), ClosePrice: 5},
		{Date: testDay(2, 1), ClosePrice: 3},
		{Date: testDay(2, 1), ClosePrice: 2},
	}}
	b := SourceSeries{Source: "x", Points: []Point{
		{Date: testDay(2, 1), ClosePrice: 3},
		{Date: testDay(2, 1), ClosePrice: 1},
		{Date: testDay(2, 1), ClosePrice: 2},
		{Date: testDay(2, 1), ClosePrice: 5},
	}}

	da, _, _ := dedupeSource(a)
	db, _, _ := dedupeSource(b)
	if da[testDay(2, 1)] != db[testDay(2, 1)] {
		t.Fatalf("dedupe depends on scrape order: %v vs %v", da, db)
	}
	if got := da[testDay(2, 1)]; got != 2.5 {
		t.Fatalf("expected median 2.5, got %v", got)
	}
}

func TestMedian(t *testing.T) {
	if got := median(nil); got != 0 {
		t.Errorf("empty median: expected 0, got %v", got)
	}
	if got := median([]float64{3, 1, 2}); got != 2 {
		t.Errorf("odd median: expected 2, got %v", got)
	}
	if got := median([]float64{4, 1}); got != 2.5 {
		t.Errorf("even median: expected 2.5, got %v", got)
	}
}
