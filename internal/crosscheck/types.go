// Package crosscheck performs cross-source consistency checks over the raw
// price series returned by the scrapers and the version information held in
// the local cache.
//
// The package is deliberately side-effect free: it never issues network
// requests and never writes to the cache. Its single entry point is
// Checker.Check; the query path does not invoke it.
package crosscheck

import (
	"time"
)

// SummaryStrategy selects how the per-day summary value is computed from the
// values reported by the different sources for the same day.
type SummaryStrategy string

const (
	// StrategyUnset means no summary is requested. All cross-source values are
	// still retained, but no summary value is produced. This is the default and
	// leaves callers behaving exactly as they did before cross-checking existed.
	StrategyUnset SummaryStrategy = ""

	// StrategyFirst picks the value of the source that sorts first by name.
	StrategyFirst SummaryStrategy = "first"

	// StrategyMedian picks the median of the source values (mean of the two
	// middle values when an even number of sources reports a value).
	StrategyMedian SummaryStrategy = "median"

	// StrategyLast picks the value of the source that sorts last by name.
	StrategyLast SummaryStrategy = "last"
)

// Point is one raw price observation as returned by a source.
type Point struct {
	Date       time.Time
	ClosePrice float64
}

// SourceSeries is the raw series returned by a single source for one asset
// and interval. Version is the version the source currently attributes to
// the series; it is compared against the cached version.
type SourceSeries struct {
	Source  string
	Version string
	Points  []Point
}

// CacheVersion records the version stored in the local cache for one source.
type CacheVersion struct {
	Source  string
	Version string
}

// CachePoint is one price record held in the local cache.
type CachePoint struct {
	Date       time.Time
	ClosePrice float64
}

// CacheSourceSeries is the cached view of one source's series.
type CacheSourceSeries struct {
	Source  string
	Version string
	Points  []CachePoint
}

// CacheSnapshot is the cached view of one asset and interval across sources.
type CacheSnapshot struct {
	Versions []CacheVersion
	Series   []CacheSourceSeries
}

// CacheProvider abstracts reading the local cache. Implementations are
// expected to surface an unreadable cache directory as an error so that the
// preflight check can reject the run before any work is done.
type CacheProvider interface {
	Load(symbol string, from, to time.Time) (*CacheSnapshot, error)
}

// Request is the input of a cross-check run.
type Request struct {
	Symbol string
	From   time.Time
	To     time.Time
	Series []SourceSeries
	// Threshold is the maximum tolerated relative difference between two
	// source values on the same day, as a fraction (e.g. 0.05 for 5%).
	// Zero or negative disables deviation reporting.
	Threshold float64
	// Strategy selects the per-day summary value. StrategyUnset preserves
	// the historical behaviour of emitting no summary value.
	Strategy SummaryStrategy
}

// SourceValue is one source's value on an aligned day.
type SourceValue struct {
	Source     string  `json:"source"`
	ClosePrice float64 `json:"closePrice"`
	Version    string  `json:"version,omitempty"`
}

// AlignedDay is the aligned view of one calendar day across all sources.
type AlignedDay struct {
	Date          time.Time     `json:"date"`
	Values        []SourceValue `json:"values"`
	SummaryValue  float64       `json:"summaryValue,omitempty"`
	SummarySource string        `json:"summarySource,omitempty"`
	HasSummary    bool          `json:"hasSummary"`
}

// GapSegment is a run of consecutive calendar days missing from a source.
type GapSegment struct {
	Source          string    `json:"source"`
	From            time.Time `json:"from"`
	To              time.Time `json:"to"`
	Days            int       `json:"days"`
	Fillable        bool      `json:"fillable"`
	FillableSources []string  `json:"fillableSources,omitempty"`
}

// Deviation is one same-day pair of source values whose relative difference
// strictly exceeds the configured threshold.
type Deviation struct {
	Date               time.Time `json:"date"`
	SourceA            string    `json:"sourceA"`
	SourceB            string    `json:"sourceB"`
	ValueA             float64   `json:"valueA"`
	ValueB             float64   `json:"valueB"`
	RelativeDifference float64   `json:"relativeDifference"`
}

// StaleSegment marks a date range of a source whose cached version does not
// match the version the source currently reports.
type StaleSegment struct {
	Source        string    `json:"source"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	CachedVersion string    `json:"cachedVersion"`
	SourceVersion string    `json:"sourceVersion"`
}

// SourceStat summarizes one source's contribution to the run.
type SourceStat struct {
	Source         string `json:"source"`
	HitDays        int    `json:"hitDays"`
	DuplicateDays  int    `json:"duplicateDays"`
	CurrentVersion string `json:"currentVersion,omitempty"`
	CachedVersion  string `json:"cachedVersion,omitempty"`
	VersionStale   bool   `json:"versionStale"`
}

// Summary is the structured, machine-readable digest of a run.
type Summary struct {
	Symbol         string          `json:"symbol"`
	From           time.Time       `json:"from"`
	To             time.Time       `json:"to"`
	SourceHitDays  map[string]int  `json:"sourceHitDays"`
	GapSegments    int             `json:"gapSegments"`
	DeviationCount int             `json:"deviationCount"`
	StaleSegments  int             `json:"staleSegments"`
	Strategy       SummaryStrategy `json:"strategy,omitempty"`
	Threshold      float64         `json:"threshold,omitempty"`
}

// Report is the full output of a cross-check run.
type Report struct {
	Symbol      string         `json:"symbol"`
	From        time.Time      `json:"from"`
	To          time.Time      `json:"to"`
	Aligned     []AlignedDay   `json:"aligned"`
	Gaps        []GapSegment   `json:"gaps"`
	Deviations  []Deviation    `json:"deviations"`
	Stale       []StaleSegment `json:"staleSegments"`
	SourceStats []SourceStat   `json:"sourceStats"`
	Summary     Summary        `json:"summary"`
	// HumanSummary is the human-readable rendering of Summary and always
	// carries the same interval, hit days, gap count and deviation count.
	HumanSummary string `json:"humanSummary"`
}

// ErrorKind categorizes the preflight failures rejected before any
// cross-check work starts.
type ErrorKind string

const (
	ErrUnknownSymbol   ErrorKind = "unknown_symbol"
	ErrInvalidRange    ErrorKind = "invalid_range"
	ErrCacheUnreadable ErrorKind = "cache_unreadable"
	ErrInvalidStrategy ErrorKind = "invalid_strategy"
)

// Error is a preflight validation error.
type Error struct {
	Kind    ErrorKind
	Message string
}

func (e *Error) Error() string { return e.Message }
