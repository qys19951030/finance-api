package crosscheck

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildStats(t *testing.T) {
	deduped := map[string]map[time.Time]float64{
		"yahoo": {testDay(1, 1): 1, testDay(1, 2): 2},
		"tefas": {testDay(1, 1): 10},
	}
	duplicates := map[string]int{"yahoo": 3}
	current := map[string]string{"tefas": "v2", "yahoo": "v5"}
	cached := map[string]string{"tefas": "v1", "yahoo": "v5"}

	stats := buildStats(deduped, duplicates, current, cached)

	if len(stats) != 2 || stats[0].Source != "tefas" || stats[1].Source != "yahoo" {
		t.Fatalf("stats must be source-sorted: %+v", stats)
	}
	if stats[0].HitDays != 1 || stats[1].HitDays != 2 {
		t.Fatalf("hit days mismatch: %+v", stats)
	}
	if stats[1].DuplicateDays != 3 {
		t.Fatalf("duplicate count mismatch: %+v", stats[1])
	}
	if !stats[0].VersionStale || stats[1].VersionStale {
		t.Fatalf("stale flags mismatch: %+v", stats)
	}
}

func TestBuildSummaryAndRender(t *testing.T) {
	req := Request{
		Symbol:    "AAA",
		From:      testDay(1, 1),
		To:        testDay(1, 10),
		Threshold: 0.05,
		Strategy:  StrategyMedian,
	}
	stats := []SourceStat{
		{Source: "tefas", HitDays: 8},
		{Source: "yahoo", HitDays: 10},
	}
	gaps := []GapSegment{{}, {}}
	deviations := []Deviation{{}}
	stale := []StaleSegment{{}, {}, {}}

	s := buildSummary(req, stats, gaps, deviations, stale)

	if s.GapSegments != 2 || s.DeviationCount != 1 || s.StaleSegments != 3 {
		t.Fatalf("counts mismatch: %+v", s)
	}
	if s.SourceHitDays["tefas"] != 8 || s.SourceHitDays["yahoo"] != 10 {
		t.Fatalf("hit day map mismatch: %+v", s.SourceHitDays)
	}
	if !s.From.Equal(req.From) || !s.To.Equal(req.To) || s.Symbol != "AAA" {
		t.Fatalf("interval/symbol mismatch: %+v", s)
	}

	human := renderSummary(s)
	for _, want := range []string{
		"AAA",
		"2024-01-01",
		"2024-01-10",
		"tefas: 8",
		"yahoo: 10",
		"gap segments: 2",
		"deviations: 1",
		"stale cache segments: 3",
		"summary strategy: median",
		"deviation threshold: 0.0500",
	} {
		if !strings.Contains(human, want) {
			t.Fatalf("human summary %q missing %q", human, want)
		}
	}

	// The two views must agree on every headline number.
	if !strings.Contains(human, "gap segments: "+strconv.Itoa(s.GapSegments)) {
		t.Fatal("human/structured gap count disagreement")
	}
}

func TestRenderSummary_OmitsUnsetOptions(t *testing.T) {
	s := Summary{
		Symbol:        "X",
		From:          testDay(1, 1),
		To:            testDay(1, 2),
		SourceHitDays: map[string]int{"tefas": 2},
	}
	human := renderSummary(s)
	if strings.Contains(human, "summary strategy") || strings.Contains(human, "threshold") {
		t.Fatalf("unset options must not be rendered: %q", human)
	}
}
