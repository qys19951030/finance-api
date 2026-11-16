package crosscheck

import (
	"testing"
	"time"
)

func sampleDeduped() map[string]map[time.Time]float64 {
	return map[string]map[time.Time]float64{
		"yahoo": {
			testDay(1, 1): 100,
			testDay(1, 2): 110,
		},
		"tefas": {
			testDay(1, 1): 10,
			testDay(1, 2): 11,
			testDay(1, 3): 12,
		},
	}
}

func TestAlign_SortsSourcesAndDays(t *testing.T) {
	from, to := testDay(1, 1), testDay(1, 4)
	days := align(from, to, sampleDeduped(), nil, StrategyUnset)

	if len(days) != 3 {
		t.Fatalf("expected 3 aligned days, got %d", len(days))
	}
	if !days[0].Date.Equal(testDay(1, 1)) || !days[2].Date.Equal(testDay(1, 3)) {
		t.Fatalf("days not in calendar order: %v", days)
	}
	// Sources sorted by name: tefas before yahoo.
	if days[0].Values[0].Source != "tefas" || days[0].Values[1].Source != "yahoo" {
		t.Fatalf("sources not sorted by name: %v", days[0].Values)
	}
	// Day 3 only exists in tefas; all raw values are retained.
	if len(days[2].Values) != 1 || days[2].Values[0].Source != "tefas" {
		t.Fatalf("expected single tefas value on day 3, got %v", days[2].Values)
	}
	// Unset strategy: no summary.
	if days[0].HasSummary {
		t.Fatalf("unexpected summary with unset strategy")
	}
}

func TestSummarizeStrategies(t *testing.T) {
	// AlignedDay values are always source-sorted by align(); summarize
	// relies on that contract.
	day := AlignedDay{Values: []SourceValue{
		{Source: "isyatirim", ClosePrice: 20},
		{Source: "tefas", ClosePrice: 10},
		{Source: "yahoo", ClosePrice: 100},
	}}

	if v, src, ok := summarize(day, StrategyUnset); ok {
		t.Fatalf("unset strategy must produce no summary, got %v/%s", v, src)
	}
	if v, src, ok := summarize(day, StrategyFirst); !ok || v != 20 || src != "isyatirim" {
		t.Fatalf("first: got %v/%s/%v", v, src, ok)
	}
	if v, src, ok := summarize(day, StrategyLast); !ok || v != 100 || src != "yahoo" {
		t.Fatalf("last: got %v/%s/%v", v, src, ok)
	}
	if v, src, ok := summarize(day, StrategyMedian); !ok || v != 20 || src != "median" {
		t.Fatalf("median: got %v/%s/%v", v, src, ok)
	}
}

func TestSummarize_EvenMedian(t *testing.T) {
	day := AlignedDay{Values: []SourceValue{
		{Source: "a", ClosePrice: 10},
		{Source: "b", ClosePrice: 20},
	}}
	v, _, ok := summarize(day, StrategyMedian)
	if !ok || v != 15 {
		t.Fatalf("expected median 15, got %v (ok=%v)", v, ok)
	}
}

func TestAlign_AttachesVersions(t *testing.T) {
	versions := map[string]string{"tefas": "v9"}
	days := align(testDay(1, 1), testDay(1, 1), sampleDeduped(), versions, StrategyFirst)
	for _, sv := range days[0].Values {
		if sv.Source == "tefas" && sv.Version != "v9" {
			t.Fatalf("expected tefas version v9, got %q", sv.Version)
		}
	}
	if !days[0].HasSummary || days[0].SummarySource != "tefas" || days[0].SummaryValue != 10 {
		t.Fatalf("first summary mismatch: %+v", days[0])
	}
}
