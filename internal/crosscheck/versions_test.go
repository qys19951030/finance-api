package crosscheck

import (
	"testing"
	"time"
)

type fakeCache struct{ snap *CacheSnapshot }

func (f fakeCache) Load(string, time.Time, time.Time) (*CacheSnapshot, error) {
	return f.snap, nil
}

func TestCheckVersions(t *testing.T) {
	req := Request{
		Symbol: "AAA",
		From:   testDay(1, 1),
		To:     testDay(1, 7),
		Series: []SourceSeries{
			{Source: "tefas", Version: "v2"},
			{Source: "yahoo", Version: "v5"},
			{Source: "isyatirim", Version: "v1"},
		},
	}
	snap := &CacheSnapshot{
		Versions: []CacheVersion{
			{Source: "tefas", Version: "v1"},   // stale
			{Source: "yahoo", Version: "v5"},   // up to date
			{Source: "isyatirim", Version: ""}, // unknown cached version: cannot judge
		},
		Series: []CacheSourceSeries{
			{
				Source: "tefas", Version: "v1",
				Points: []CachePoint{
					{Date: testDay(1, 1)}, {Date: testDay(1, 2)},
					// gap on day 3, then day 4
					{Date: testDay(1, 4)},
					// duplicate day must be collapsed
					{Date: testDay(1, 4)},
				},
			},
		},
	}

	c := NewChecker(fakeCache{snap: snap})
	stale, cached := c.checkVersions(req, nil, snap)

	if got := cached["tefas"]; got != "v1" {
		t.Fatalf("expected cached version map to retain v1, got %q", got)
	}
	if len(stale) != 2 {
		t.Fatalf("expected 2 stale segments (split by the day-3 gap), got %d: %+v", len(stale), stale)
	}
	if stale[0].Source != "tefas" || stale[0].CachedVersion != "v1" || stale[0].SourceVersion != "v2" {
		t.Fatalf("stale segment identity mismatch: %+v", stale[0])
	}
	if !stale[0].From.Equal(testDay(1, 1)) || !stale[0].To.Equal(testDay(1, 2)) {
		t.Fatalf("first stale range mismatch: %+v", stale[0])
	}
	if !stale[1].From.Equal(testDay(1, 4)) || !stale[1].To.Equal(testDay(1, 4)) {
		t.Fatalf("second stale range mismatch: %+v", stale[1])
	}
}

func TestCheckVersions_SkippedWithoutCache(t *testing.T) {
	c := NewChecker(nil)
	req := Request{Series: []SourceSeries{{Source: "tefas", Version: "v9"}}}
	stale, cached := c.checkVersions(req, nil, nil)
	if len(stale) != 0 || len(cached) != 0 {
		t.Fatalf("expected skipped version check, got %+v / %+v", stale, cached)
	}
}

func TestConsecutiveDays(t *testing.T) {
	ranges := consecutiveDays([]time.Time{
		testDay(2, 1), testDay(2, 2), testDay(2, 3),
		testDay(2, 5), testDay(2, 6),
	})
	if len(ranges) != 2 {
		t.Fatalf("expected 2 ranges, got %+v", ranges)
	}
	if !ranges[0].from.Equal(testDay(2, 1)) || !ranges[0].to.Equal(testDay(2, 3)) {
		t.Fatalf("range 0 mismatch: %+v", ranges[0])
	}
	if !ranges[1].from.Equal(testDay(2, 5)) || !ranges[1].to.Equal(testDay(2, 6)) {
		t.Fatalf("range 1 mismatch: %+v", ranges[1])
	}
	if ranges := consecutiveDays(nil); ranges != nil {
		t.Fatalf("nil input should yield nil, got %+v", ranges)
	}
}
