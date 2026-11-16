package crosscheck

import (
	"context"
	"errors"
	"testing"
	"time"
)

func testDay(m time.Month, d int) time.Time {
	return time.Date(2024, m, d, 0, 0, 0, 0, time.UTC)
}

type failingCache struct{ err error }

func (f failingCache) Load(string, time.Time, time.Time) (*CacheSnapshot, error) {
	return nil, f.err
}

func TestPreflight(t *testing.T) {
	from, to := testDay(1, 1), testDay(1, 10)
	okSeries := []SourceSeries{{
		Source: "tefas",
		Points: []Point{{Date: testDay(1, 2), ClosePrice: 10}},
	}}

	tests := []struct {
		name    string
		checker *Checker
		req     Request
		want    ErrorKind
	}{
		{
			name:    "reversed range",
			checker: NewChecker(nil),
			req:     Request{Symbol: "AAA", From: to, To: from, Series: okSeries},
			want:    ErrInvalidRange,
		},
		{
			name:    "zero range",
			checker: NewChecker(nil),
			req:     Request{Symbol: "AAA", To: to, Series: okSeries},
			want:    ErrInvalidRange,
		},
		{
			name:    "unknown strategy",
			checker: NewChecker(nil),
			req:     Request{Symbol: "AAA", From: from, To: to, Series: okSeries, Strategy: "weird"},
			want:    ErrInvalidStrategy,
		},
		{
			name:    "no source has the symbol",
			checker: NewChecker(nil),
			req:     Request{Symbol: "AAA", From: from, To: to, Series: []SourceSeries{{Source: "tefas"}}},
			want:    ErrUnknownSymbol,
		},
		{
			name:    "no series at all",
			checker: NewChecker(nil),
			req:     Request{Symbol: "AAA", From: from, To: to},
			want:    ErrUnknownSymbol,
		},
		{
			name:    "cache directory unreadable",
			checker: NewChecker(failingCache{err: errors.New("open /data: permission denied")}),
			req:     Request{Symbol: "AAA", From: from, To: to, Series: okSeries},
			want:    ErrCacheUnreadable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap, err := tt.checker.preflight(context.Background(), tt.req)
			if err == nil {
				t.Fatalf("expected error of kind %s, got nil", tt.want)
			}
			var ce *Error
			if !errors.As(err, &ce) {
				t.Fatalf("expected *crosscheck.Error, got %T: %v", err, err)
			}
			if ce.Kind != tt.want {
				t.Fatalf("expected kind %s, got %s (%s)", tt.want, ce.Kind, ce.Message)
			}
			if snap != nil {
				t.Fatalf("expected nil snapshot on error")
			}
		})
	}
}

func TestPreflight_ValidRequest(t *testing.T) {
	c := NewChecker(nil)
	req := Request{
		Symbol:   "AAA",
		From:     testDay(1, 1),
		To:       testDay(1, 10),
		Series:   []SourceSeries{{Source: "tefas", Points: []Point{{Date: testDay(1, 2), ClosePrice: 10}}}},
		Strategy: StrategyMedian,
	}
	snap, err := c.preflight(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap != nil {
		t.Fatalf("expected nil snapshot without cache provider")
	}
}
