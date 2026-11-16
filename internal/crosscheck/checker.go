package crosscheck

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const dateLayout = "2006-01-02"

// Checker runs cross-source consistency checks. It is safe for concurrent
// use because Check holds no mutable state between runs.
type Checker struct {
	cache CacheProvider
}

// NewChecker builds a Checker. cache may be nil, in which case version
// checks are skipped; pass an implementation to enable them.
func NewChecker(cache CacheProvider) *Checker {
	return &Checker{cache: cache}
}

// Check validates the request preconditions, aligns the raw source series,
// and produces a difference report. It never performs network requests or
// cache writes.
func (c *Checker) Check(ctx context.Context, req Request) (*Report, error) {
	snap, err := c.preflight(ctx, req)
	if err != nil {
		return nil, err
	}

	from, to := day(req.From), day(req.To)

	// Same-source/same-day convergence with an explicit, order-independent
	// rule. Repeated entries for one source are merged first so that entry
	// order can never decide which points survive.
	deduped := make(map[string]map[time.Time]float64, len(req.Series))
	duplicates := make(map[string]int, len(req.Series))
	versions := make(map[string]string, len(req.Series))
	merged := make(map[string][]Point)
	versionSeen := make(map[string]string)
	for _, s := range req.Series {
		merged[s.Source] = append(merged[s.Source], s.Points...)
		if s.Version != "" {
			versionSeen[s.Source] = s.Version
		}
	}
	for src, points := range merged {
		converged, _, dup := dedupeSource(SourceSeries{Source: src, Points: points})
		deduped[src] = converged
		duplicates[src] = dup
		versions[src] = versionSeen[src]
	}

	aligned := align(from, to, deduped, versions, req.Strategy)
	gaps := findGaps(from, to, deduped)
	deviations := findDeviations(aligned, req.Threshold)
	stale, cachedVersions := c.checkVersions(req, deduped, snap)
	stats := buildStats(deduped, duplicates, versions, cachedVersions)
	summary := buildSummary(req, stats, gaps, deviations, stale)

	return &Report{
		Symbol:       req.Symbol,
		From:         from,
		To:           to,
		Aligned:      aligned,
		Gaps:         gaps,
		Deviations:   deviations,
		Stale:        stale,
		SourceStats:  stats,
		Summary:      summary,
		HumanSummary: renderSummary(summary),
	}, nil
}

// day truncates a timestamp to the UTC calendar day, matching the
// normalization already applied by the scrapers.
func day(t time.Time) time.Time {
	return t.UTC().Truncate(24 * time.Hour)
}

// validStrategy reports whether s is one of the supported summary strategies.
func validStrategy(s SummaryStrategy) bool {
	switch s {
	case StrategyUnset, StrategyFirst, StrategyMedian, StrategyLast:
		return true
	default:
		return false
	}
}

// preflight rejects invalid requests before any work begins. It returns
// *Error so callers can distinguish the four precondition failures, and the
// cache snapshot when a provider is configured so the rest of the run does
// not touch the cache again.
func (c *Checker) preflight(ctx context.Context, req Request) (*CacheSnapshot, error) {
	if req.From.IsZero() || req.To.IsZero() {
		return nil, &Error{
			Kind:    ErrInvalidRange,
			Message: "invalid range: both from and to dates are required",
		}
	}

	from, to := day(req.From), day(req.To)
	if from.After(to) {
		return nil, &Error{
			Kind: ErrInvalidRange,
			Message: "invalid range: from " + from.Format(dateLayout) +
				" is after to " + to.Format(dateLayout),
		}
	}

	if !validStrategy(req.Strategy) {
		return nil, &Error{
			Kind:    ErrInvalidStrategy,
			Message: "invalid summary strategy: " + string(req.Strategy),
		}
	}

	found := false
	for _, s := range req.Series {
		if len(s.Points) > 0 {
			found = true
			break
		}
	}
	if !found {
		return nil, &Error{
			Kind:    ErrUnknownSymbol,
			Message: "symbol " + req.Symbol + " was not found in any source",
		}
	}

	if c.cache == nil {
		return nil, nil
	}
	snap, err := c.cache.Load(req.Symbol, from, to)
	if err != nil {
		return nil, &Error{
			Kind: ErrCacheUnreadable,
			Message: "cache for symbol " + req.Symbol + " could not be read: " +
				err.Error(),
		}
	}
	return snap, nil
}

// dedupeSource collapses same-source, same-day duplicates with an explicit,
// order-independent rule and returns the valid-day map together with the
// number of duplicate observations that were collapsed.
//
// Convergence rule: when a source reports several observations for one day,
// the retained value is the median of the observed prices (the mean of the
// two middle values for an even count). Sorting by value makes the result
// independent of the order in which the observations were scraped. Hit days
// are counted after convergence, so a duplicated day still counts once.
func dedupeSource(series SourceSeries) (map[time.Time]float64, int, int) {
	grouped := make(map[time.Time][]float64)
	for _, p := range series.Points {
		grouped[day(p.Date)] = append(grouped[day(p.Date)], p.ClosePrice)
	}

	deduped := make(map[time.Time]float64, len(grouped))
	duplicates := 0
	for d, values := range grouped {
		if len(values) > 1 {
			duplicates += len(values) - 1
		}
		deduped[d] = median(values)
	}
	return deduped, len(deduped), duplicates
}

// median returns the median of the given values without mutating the input.
func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// align merges the deduplicated per-source maps into calendar-ordered days.
// Only days present in at least one source are emitted; within a day the
// source values are always sorted by source name so the output never depends
// on scrape or iteration order.
func align(from, to time.Time, deduped map[string]map[time.Time]float64, versions map[string]string, strategy SummaryStrategy) []AlignedDay {
	sourceNames := make([]string, 0, len(deduped))
	for src := range deduped {
		sourceNames = append(sourceNames, src)
	}
	sort.Strings(sourceNames)

	days := make([]AlignedDay, 0)
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		values := make([]SourceValue, 0, len(sourceNames))
		for _, src := range sourceNames {
			if v, ok := deduped[src][d]; ok {
				values = append(values, SourceValue{
					Source:     src,
					ClosePrice: v,
					Version:    versions[src],
				})
			}
		}
		if len(values) == 0 {
			continue
		}

		day := AlignedDay{Date: d, Values: values}
		if value, source, ok := summarize(day, strategy); ok {
			day.SummaryValue = value
			day.SummarySource = source
			day.HasSummary = true
		}
		days = append(days, day)
	}
	return days
}

// summarize computes the summary value for one day according to strategy.
// ok is false when no summary was requested. Sources are already sorted by
// name inside AlignedDay, so first/last are deterministic.
func summarize(day AlignedDay, strategy SummaryStrategy) (value float64, source string, ok bool) {
	switch strategy {
	case StrategyFirst:
		return day.Values[0].ClosePrice, day.Values[0].Source, true
	case StrategyLast:
		last := day.Values[len(day.Values)-1]
		return last.ClosePrice, last.Source, true
	case StrategyMedian:
		prices := make([]float64, len(day.Values))
		for i, v := range day.Values {
			prices[i] = v.ClosePrice
		}
		m := median(prices)
		if len(prices) == 1 {
			return m, day.Values[0].Source, true
		}
		return m, "median", true
	default:
		return 0, "", false
	}
}

// findGaps turns per-source missing days into merged, consecutive segments.
//
// A gap is any calendar day inside [from, to] absent from a source's
// deduplicated series. Consecutive missing days form one segment; a single
// day carrying a value breaks the segment, so gaps on either side of it are
// reported separately. Each segment records whether another source holds a
// value for the missing days and which sources could fill them.
func findGaps(from, to time.Time, deduped map[string]map[time.Time]float64) []GapSegment {
	sourceNames := make([]string, 0, len(deduped))
	for src := range deduped {
		sourceNames = append(sourceNames, src)
	}
	sort.Strings(sourceNames)

	gaps := make([]GapSegment, 0)
	for _, src := range sourceNames {
		own := deduped[src]

		var segStart, segEnd time.Time
		segLen := 0
		flush := func(d time.Time) {
			if segLen == 0 {
				return
			}
			fillable := make([]string, 0)
			for _, other := range sourceNames {
				if other == src {
					continue
				}
				hasAny := false
				for g := segStart; !g.After(segEnd); g = g.AddDate(0, 0, 1) {
					if _, ok := deduped[other][g]; ok {
						hasAny = true
						break
					}
				}
				if hasAny {
					fillable = append(fillable, other)
				}
			}
			gaps = append(gaps, GapSegment{
				Source:          src,
				From:            segStart,
				To:              segEnd,
				Days:            segLen,
				Fillable:        len(fillable) > 0,
				FillableSources: fillable,
			})
			segLen = 0
		}

		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			if _, ok := own[d]; ok {
				// A valued day closes any open segment before it.
				flush(d)
				continue
			}
			if segLen == 0 {
				segStart = d
			}
			segEnd = d
			segLen++
		}
		flush(to)
	}
	return gaps
}

// findDeviations flags every same-day source pair whose relative difference
// strictly exceeds threshold. The relative difference is |a-b|/min(|a|,|b|),
// i.e. the spread relative to the lower of the two values; a difference
// exactly equal to threshold is not a deviation. Days where either value is
// zero cannot produce a meaningful relative difference and are skipped.
// Values inside an AlignedDay are source-sorted, so every emitted pair names
// the lexicographically smaller source first.
func findDeviations(aligned []AlignedDay, threshold float64) []Deviation {
	if threshold <= 0 {
		return nil
	}

	deviations := make([]Deviation, 0)
	for _, day := range aligned {
		values := day.Values
		for i := 0; i < len(values); i++ {
			for j := i + 1; j < len(values); j++ {
				a, b := values[i], values[j]
				if a.ClosePrice == 0 || b.ClosePrice == 0 {
					continue
				}
				base := math.Min(math.Abs(a.ClosePrice), math.Abs(b.ClosePrice))
				if base <= 0 {
					continue
				}
				rel := math.Abs(a.ClosePrice-b.ClosePrice) / base
				if rel <= threshold {
					continue
				}
				deviations = append(deviations, Deviation{
					Date:               day.Date,
					SourceA:            a.Source,
					SourceB:            b.Source,
					ValueA:             a.ClosePrice,
					ValueB:             b.ClosePrice,
					RelativeDifference: rel,
				})
			}
		}
	}
	return deviations
}

// checkVersions compares the version held in the cache with the version each
// source currently reports. A source is stale when both versions are known
// and differ; its stale range covers the cached records inside [from, to],
// merged into consecutive segments so callers never hand out old data as if
// it were current. Without a cache provider the check is simply skipped.
func (c *Checker) checkVersions(req Request, deduped map[string]map[time.Time]float64, snap *CacheSnapshot) ([]StaleSegment, map[string]string) {
	cachedVersions := make(map[string]string)
	if snap != nil {
		for _, v := range snap.Versions {
			cachedVersions[v.Source] = v.Version
		}
	}
	currentVersions := make(map[string]string, len(req.Series))
	for _, s := range req.Series {
		currentVersions[s.Source] = s.Version
	}

	cachedDays := make(map[string][]time.Time)
	if snap != nil {
		for _, cs := range snap.Series {
			days := make([]time.Time, 0, len(cs.Points))
			seen := make(map[time.Time]bool)
			for _, p := range cs.Points {
				d := day(p.Date)
				if d.Before(day(req.From)) || d.After(day(req.To)) || seen[d] {
					continue
				}
				seen[d] = true
				days = append(days, d)
			}
			sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
			cachedDays[cs.Source] = days
		}
	}

	stale := make([]StaleSegment, 0)
	sourceNames := make([]string, 0, len(req.Series))
	for _, s := range req.Series {
		sourceNames = append(sourceNames, s.Source)
	}
	sort.Strings(sourceNames)

	for _, src := range sourceNames {
		current := currentVersions[src]
		cached := cachedVersions[src]
		if current == "" || cached == "" || current == cached {
			continue
		}
		for _, seg := range consecutiveDays(cachedDays[src]) {
			stale = append(stale, StaleSegment{
				Source:        src,
				From:          seg.from,
				To:            seg.to,
				CachedVersion: cached,
				SourceVersion: current,
			})
		}
	}
	return stale, cachedVersions
}

// dayRange is a closed calendar interval.
type dayRange struct{ from, to time.Time }

// consecutiveDays merges sorted unique days into maximal consecutive runs.
func consecutiveDays(days []time.Time) []dayRange {
	if len(days) == 0 {
		return nil
	}
	ranges := make([]dayRange, 0)
	start, end := days[0], days[0]
	for _, d := range days[1:] {
		if d.Equal(end.AddDate(0, 0, 1)) {
			end = d
			continue
		}
		ranges = append(ranges, dayRange{start, end})
		start, end = d, d
	}
	ranges = append(ranges, dayRange{start, end})
	return ranges
}

// buildStats computes one SourceStat per source, ordered by source name.
// Hit days come from the deduplicated series, so same-day duplicates count
// once; duplicate observation counts are reported alongside for visibility.
func buildStats(deduped map[string]map[time.Time]float64, duplicates map[string]int, currentVersions, cachedVersions map[string]string) []SourceStat {
	sourceNames := make([]string, 0, len(deduped))
	for src := range deduped {
		sourceNames = append(sourceNames, src)
	}
	sort.Strings(sourceNames)

	stats := make([]SourceStat, 0, len(sourceNames))
	for _, src := range sourceNames {
		current := currentVersions[src]
		cached := cachedVersions[src]
		stats = append(stats, SourceStat{
			Source:         src,
			HitDays:        len(deduped[src]),
			DuplicateDays:  duplicates[src],
			CurrentVersion: current,
			CachedVersion:  cached,
			VersionStale:   current != "" && cached != "" && current != cached,
		})
	}
	return stats
}

// buildSummary assembles the structured summary whose counts mirror the
// detailed report exactly.
func buildSummary(req Request, stats []SourceStat, gaps []GapSegment, deviations []Deviation, stale []StaleSegment) Summary {
	hits := make(map[string]int, len(stats))
	for _, st := range stats {
		hits[st.Source] = st.HitDays
	}
	return Summary{
		Symbol:         req.Symbol,
		From:           day(req.From),
		To:             day(req.To),
		SourceHitDays:  hits,
		GapSegments:    len(gaps),
		DeviationCount: len(deviations),
		StaleSegments:  len(stale),
		Strategy:       req.Strategy,
		Threshold:      req.Threshold,
	}
}

// renderSummary renders the structured summary as a deterministic,
// human-readable text. Every number it prints comes straight from the
// structured summary, so the two views cannot drift apart.
func renderSummary(s Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "cross-check summary for %s from %s to %s",
		s.Symbol, s.From.Format(dateLayout), s.To.Format(dateLayout))

	sources := make([]string, 0, len(s.SourceHitDays))
	for src := range s.SourceHitDays {
		sources = append(sources, src)
	}
	sort.Strings(sources)
	b.WriteString("\nsource hit days:")
	for _, src := range sources {
		fmt.Fprintf(&b, "\n  %s: %d", src, s.SourceHitDays[src])
	}
	fmt.Fprintf(&b, "\ngap segments: %d", s.GapSegments)
	fmt.Fprintf(&b, "\ndeviations: %d", s.DeviationCount)
	fmt.Fprintf(&b, "\nstale cache segments: %d", s.StaleSegments)
	if s.Strategy != StrategyUnset {
		fmt.Fprintf(&b, "\nsummary strategy: %s", s.Strategy)
	}
	if s.Threshold > 0 {
		fmt.Fprintf(&b, "\ndeviation threshold: %.4f", s.Threshold)
	}
	return b.String()
}
