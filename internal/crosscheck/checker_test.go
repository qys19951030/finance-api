package crosscheck

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCheck_PreflightFailure(t *testing.T) {
	c := NewChecker(nil)
	_, err := c.Check(context.Background(), Request{})
	var ce *Error
	if !errors.As(err, &ce) || ce.Kind != ErrInvalidRange {
		t.Fatalf("expected invalid range error, got %v", err)
	}
}

func TestCheck_EndToEnd(t *testing.T) {
	from, to := testDay(1, 1), testDay(1, 5)
	req := Request{
		Symbol:    "AAA",
		From:      from,
		To:        to,
		Threshold: 0.05,
		Strategy:  StrategyMedian,
		Series: []SourceSeries{
			{
				Source:  "tefas",
				Version: "v2",
				Points: []Point{
					{Date: testDay(1, 1), ClosePrice: 10},
					{Date: testDay(1, 2), ClosePrice: 11},
					// duplicated day 2 with different scrape order
					{Date: testDay(1, 2), ClosePrice: 13},
					{Date: testDay(1, 3), ClosePrice: 12},
				},
			},
			{
				Source:  "yahoo",
				Version: "v5",
				Points: []Point{
					// 10.8 vs tefas 10 on day 1 -> 8% deviation over the 5% threshold.
					{Date: testDay(1, 1), ClosePrice: 10.8},
					{Date: testDay(1, 2), ClosePrice: 12.1},
					{Date: testDay(1, 3), ClosePrice: 12.2},
					{Date: testDay(1, 4), ClosePrice: 13},
					{Date: testDay(1, 5), ClosePrice: 14},
				},
			},
		},
	}

	rep, err := NewChecker(nil).Check(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Aligned days: 1..5, every day keeps all raw source values.
	if len(rep.Aligned) != 5 {
		t.Fatalf("expected 5 aligned days, got %d", len(rep.Aligned))
	}
	day2 := rep.Aligned[1]
	if len(day2.Values) != 2 {
		t.Fatalf("day 2 should retain both sources: %+v", day2.Values)
	}
	// tefas day 2 converged to median(11, 13) = 12, independent of order.
	if day2.Values[0].Source != "tefas" || day2.Values[0].ClosePrice != 12 {
		t.Fatalf("tefas day 2 convergence mismatch: %+v", day2.Values[0])
	}
	// Median summary across the two sources on day 2: median(12, 12.1) = 12.05.
	if !day2.HasSummary || day2.SummarySource != "median" || day2.SummaryValue != 12.05 {
		t.Fatalf("day 2 summary mismatch: %+v", day2)
	}

	// Hit days: tefas 3 (duplicate counts once), yahoo 5.
	if rep.Summary.SourceHitDays["tefas"] != 3 || rep.Summary.SourceHitDays["yahoo"] != 5 {
		t.Fatalf("hit days mismatch: %+v", rep.Summary.SourceHitDays)
	}
	for _, st := range rep.SourceStats {
		if st.Source == "tefas" && (st.HitDays != 3 || st.DuplicateDays != 1) {
			t.Fatalf("tefas stat mismatch: %+v", st)
		}
	}

	// Gaps: tefas missing days 4-5 as one segment, fillable by yahoo.
	if rep.Summary.GapSegments != len(rep.Gaps) || len(rep.Gaps) != 1 {
		t.Fatalf("gap count mismatch: summary=%d detail=%d", rep.Summary.GapSegments, len(rep.Gaps))
	}
	g := rep.Gaps[0]
	if g.Source != "tefas" || !g.From.Equal(testDay(1, 4)) || !g.To.Equal(testDay(1, 5)) ||
		!g.Fillable || len(g.FillableSources) != 1 || g.FillableSources[0] != "yahoo" {
		t.Fatalf("gap segment mismatch: %+v", g)
	}

	// Deviation counts must agree between detail and summary.
	if rep.Summary.DeviationCount != len(rep.Deviations) {
		t.Fatalf("deviation count disagreement")
	}
	if len(rep.Deviations) == 0 {
		t.Fatalf("expected at least one deviation (day 2: 12 vs 12.1 is fine, day 4/5 only yahoo)")
	}
	// Only days with both sources (1-3) can deviate.
	for _, d := range rep.Deviations {
		if d.Date.After(testDay(1, 3)) {
			t.Fatalf("deviation on single-source day: %+v", d)
		}
		if d.RelativeDifference <= req.Threshold {
			t.Fatalf("sub-threshold pair reported: %+v", d)
		}
	}

	// Interval mirrors into both summary views.
	if !rep.Summary.From.Equal(from) || !rep.Summary.To.Equal(to) {
		t.Fatalf("summary interval mismatch: %+v", rep.Summary)
	}
	if !strings.Contains(rep.HumanSummary, "2024-01-01") ||
		!strings.Contains(rep.HumanSummary, "2024-01-05") {
		t.Fatalf("human summary missing interval: %q", rep.HumanSummary)
	}
}

func TestCheck_DefaultStrategyHasNoSummary(t *testing.T) {
	req := Request{
		Symbol: "AAA",
		From:   testDay(1, 1),
		To:     testDay(1, 1),
		Series: []SourceSeries{{
			Source: "tefas",
			Points: []Point{{Date: testDay(1, 1), ClosePrice: 10}},
		}},
	}
	rep, err := NewChecker(nil).Check(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep.Aligned[0].HasSummary {
		t.Fatalf("default request must not produce a summary value")
	}
	if rep.Summary.Strategy != StrategyUnset {
		t.Fatalf("expected unset strategy in summary: %q", rep.Summary.Strategy)
	}
}

func TestCheck_DuplicateSourceEntriesDoNotDependOnOrder(t *testing.T) {
	base := Request{
		Symbol: "AAA",
		From:   testDay(1, 1),
		To:     testDay(1, 1),
	}
	run := func(series []SourceSeries) *Report {
		req := base
		req.Series = series
		rep, err := NewChecker(nil).Check(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return rep
	}

	first := run([]SourceSeries{
		{Source: "tefas", Points: []Point{{Date: testDay(1, 1), ClosePrice: 10}}},
		{Source: "tefas", Points: []Point{{Date: testDay(1, 1), ClosePrice: 999}}},
	})
	second := run([]SourceSeries{
		{Source: "tefas", Points: []Point{{Date: testDay(1, 1), ClosePrice: 999}}},
		{Source: "tefas", Points: []Point{{Date: testDay(1, 1), ClosePrice: 10}}},
	})

	if len(first.Summary.SourceHitDays) != 1 || len(second.Summary.SourceHitDays) != 1 {
		t.Fatalf("duplicate source entries must collapse to one source")
	}
	if first.Aligned[0].Values[0].ClosePrice != second.Aligned[0].Values[0].ClosePrice {
		t.Fatalf("result depends on source entry order: %v vs %v",
			first.Aligned[0].Values, second.Aligned[0].Values)
	}
}

func TestCheck_CacheVersionStaleness(t *testing.T) {
	snap := &CacheSnapshot{
		Versions: []CacheVersion{{Source: "tefas", Version: "v1"}},
		Series: []CacheSourceSeries{{
			Source: "tefas",
			Points: []CachePoint{
				{Date: testDay(1, 1), ClosePrice: 9},
				{Date: testDay(1, 2), ClosePrice: 9},
			},
		}},
	}
	req := Request{
		Symbol: "AAA",
		From:   testDay(1, 1),
		To:     testDay(1, 2),
		Series: []SourceSeries{{
			Source:  "tefas",
			Version: "v2",
			Points: []Point{
				{Date: testDay(1, 1), ClosePrice: 10},
				{Date: testDay(1, 2), ClosePrice: 10},
			},
		}},
	}

	rep, err := NewChecker(fakeCache{snap: snap}).Check(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep.Summary.StaleSegments != 1 || len(rep.Stale) != 1 {
		t.Fatalf("expected one stale segment, got %+v", rep.Stale)
	}
	st := rep.Stale[0]
	if st.CachedVersion != "v1" || st.SourceVersion != "v2" ||
		!st.From.Equal(testDay(1, 1)) || !st.To.Equal(testDay(1, 2)) {
		t.Fatalf("stale segment mismatch: %+v", st)
	}
	if !rep.SourceStats[0].VersionStale {
		t.Fatalf("source stat must flag stale version")
	}
	if !strings.Contains(rep.HumanSummary, "stale cache segments: 1") {
		t.Fatalf("human summary missing stale count: %q", rep.HumanSummary)
	}
}
