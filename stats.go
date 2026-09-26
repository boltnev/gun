package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type percentileQuantile struct {
	label string
	p     float64
}

// summaryPercentiles is the percentile set printed in stats blocks.
var summaryPercentiles = []percentileQuantile{
	{"p50", 50},
	{"p75", 75},
	{"p90", 90},
	{"p95", 95},
	{"p99", 99},
	{"p99.9", 99.9},
}

// Samples accumulates exact int64 observations (nanoseconds or bytes) and
// answers min/avg/max/percentile queries. Percentiles interpolate linearly
// between the two closest ranks, so a p between two observed samples reports
// their weighted share rather than snapping to the lower one.
type Samples struct {
	values []int64
	sum    int64
	sorted bool
}

func (s *Samples) Observe(v int64) {
	s.values = append(s.values, v)
	s.sum += v
	s.sorted = false
}

func (s *Samples) Count() int { return len(s.values) }

// ensureSorted sorts values ascending once, on first query after a change.
func (s *Samples) ensureSorted() {
	if !s.sorted {
		sort.Slice(s.values, func(i, j int) bool { return s.values[i] < s.values[j] })
		s.sorted = true
	}
}

func (s *Samples) Min() int64 {
	s.ensureSorted()
	if len(s.values) == 0 {
		return 0
	}
	return s.values[0]
}

func (s *Samples) Max() int64 {
	s.ensureSorted()
	if len(s.values) == 0 {
		return 0
	}
	return s.values[len(s.values)-1]
}

func (s *Samples) Avg() float64 {
	if len(s.values) == 0 {
		return 0
	}
	return float64(s.sum) / float64(len(s.values))
}

// Percentile returns the p-th percentile (0..100, clamped) of the observed
// values; with no observations it reports 0.
func (s *Samples) Percentile(p float64) float64 {
	s.ensureSorted()
	n := len(s.values)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return float64(s.values[0])
	}
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	rank := p / 100 * float64(n-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return float64(s.values[lo])
	}
	frac := rank - float64(lo)
	return float64(s.values[lo]) + frac*float64(s.values[hi]-s.values[lo])
}

func formatNs(v float64) string {
	return time.Duration(math.Round(v)).String()
}

// formatBytes rounds to whole bytes: sizes are byte counts, and interpolated
// percentiles carry float dust that would otherwise show up as 298.99999999.
func formatBytes(v float64) string {
	return strconv.FormatInt(int64(math.Round(v)), 10)
}

func statsLine(s *Samples, format func(float64) string) string {
	parts := []string{
		"min " + format(float64(s.Min())),
		"avg " + format(s.Avg()),
		"max " + format(float64(s.Max())),
	}
	for _, pq := range summaryPercentiles {
		parts = append(parts, fmt.Sprintf("%s %s", pq.label, format(s.Percentile(pq.p))))
	}
	return strings.Join(parts, "; ")
}

// DurationStatsLine formats the min/avg/max/percentile summary for duration samples.
func DurationStatsLine(s *Samples) string { return statsLine(s, formatNs) }

// SizeStatsLine formats the same summary for byte-count samples.
func SizeStatsLine(s *Samples) string { return statsLine(s, formatBytes) }
