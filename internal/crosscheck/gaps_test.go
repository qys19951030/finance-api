package crosscheck

import (
	"testing"
	"time"
)

func TestFindGaps_MergesConsecutiveAndBreaksOnValue(t *testing.T) {
	from, to := testDay(1, 1), testDay(1, 7)
	deduped := map[string]map[time.Time]float64{
		// Missing days 1-2, has day 3, missing days 4-7.
		"tefas": {
			testDay(1, 3): 12,
		},
		"yahoo": {
			testDay(1, 1): 100,
			testDay(1, 2): 101,
			testDay(1, 4): 103,
		},
	}

	gaps := findGaps(from, to, deduped)

	// tefas: days 1-2 and 4-7 (two segments split by the day-3 value);
	// yahoo: day 3 and days 5-7 (two segments split by the day-4 value).
	if len(gaps) != 4 {
		t.Fatalf("expected 4 segments, got %d: %+v", len(gaps), gaps)
	}

	seg := gaps[0]
	if seg.Source != "tefas" || !seg.From.Equal(testDay(1, 1)) || !seg.To.Equal(testDay(1, 2)) || seg.Days != 2 {
		t.Fatalf("segment 0 mismatch: %+v", seg)
	}
	if !seg.Fillable || len(seg.FillableSources) != 1 || seg.FillableSources[0] != "yahoo" {
		t.Fatalf("segment 0 should be fillable by yahoo: %+v", seg)
	}

	seg = gaps[1]
	if seg.Source != "tefas" || !seg.From.Equal(testDay(1, 4)) || !seg.To.Equal(testDay(1, 7)) || seg.Days != 4 {
		t.Fatalf("segment 1 mismatch: %+v", seg)
	}
	// Yahoo only covers day 4 inside this segment, but that still fills part.
	if !seg.Fillable {
		t.Fatalf("segment 1 should be partially fillable: %+v", seg)
	}

	seg = gaps[2]
	if seg.Source != "yahoo" || seg.Days != 1 || !seg.From.Equal(testDay(1, 3)) {
		t.Fatalf("segment 2 mismatch: %+v", seg)
	}
	if !seg.Fillable || len(seg.FillableSources) != 1 || seg.FillableSources[0] != "tefas" {
		t.Fatalf("yahoo day-3 gap should be fillable by tefas: %+v", seg)
	}

	seg = gaps[3]
	if seg.Source != "yahoo" || !seg.From.Equal(testDay(1, 5)) || !seg.To.Equal(testDay(1, 7)) || seg.Days != 3 {
		t.Fatalf("segment 3 mismatch: %+v", seg)
	}
	if seg.Fillable {
		t.Fatalf("tefas has no data on days 5-7, segment must not be fillable: %+v", seg)
	}
}

func TestFindGaps_NoGaps(t *testing.T) {
	from, to := testDay(1, 1), testDay(1, 3)
	deduped := map[string]map[time.Time]float64{
		"tefas": {
			testDay(1, 1): 1,
			testDay(1, 2): 2,
			testDay(1, 3): 3,
		},
	}
	if gaps := findGaps(from, to, deduped); len(gaps) != 0 {
		t.Fatalf("expected no gaps, got %+v", gaps)
	}
}

func TestFindGaps_SingleDayGapIsItsOwnSegment(t *testing.T) {
	from, to := testDay(1, 1), testDay(1, 3)
	deduped := map[string]map[time.Time]float64{
		"tefas": {
			testDay(1, 1): 1,
			testDay(1, 3): 3,
		},
	}
	gaps := findGaps(from, to, deduped)
	if len(gaps) != 1 {
		t.Fatalf("expected one single-day segment, got %d", len(gaps))
	}
	if gaps[0].Days != 1 || !gaps[0].From.Equal(testDay(1, 2)) || !gaps[0].To.Equal(testDay(1, 2)) {
		t.Fatalf("single-day segment mismatch: %+v", gaps[0])
	}
	if gaps[0].Fillable {
		t.Fatalf("no other source exists, must not be fillable")
	}
}
