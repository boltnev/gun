package main

import (
	"fmt"
	"math"
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

const (
	// bucketsPerDecade sets the histogram resolution: one bucket per 0.1%
	// step (10^(1/1000) ≈ 1.0023), so a reported percentile is within half
	// a bucket width of the true value.
	bucketsPerDecade = 1000
	// maxDecade caps the histogram range at 10^13: enough for ~3 hours of
	// nanoseconds or 10 TB of bytes; larger values clamp to the top bucket.
	maxDecade   = 13
	bucketCount = bucketsPerDecade*maxDecade + 1
)

// Samples is a fixed-memory streaming stats collector: min/avg/max are exact
// running values, percentiles come from a log-scaled bucket histogram by the
// nearest-rank method. Observe is O(1) and Percentile is O(bucketCount), so
// both are cheap enough for the mid-run ticker.
type Samples struct {
	count   int64
	sum     int64
	minV    int64
	maxV    int64
	buckets []int64
}

func (s *Samples) Observe(v int64) {
	if s.buckets == nil {
		s.buckets = make([]int64, bucketCount)
	}
	s.count++
	s.sum += v
	if s.count == 1 || v < s.minV {
		s.minV = v
	}
	if s.count == 1 || v > s.maxV {
		s.maxV = v
	}
	s.buckets[bucketIndex(v)]++
}

func (s *Samples) Count() int { return int(s.count) }

func (s *Samples) Min() int64 {
	if s.count == 0 {
		return 0
	}
	return s.minV
}

func (s *Samples) Max() int64 {
	if s.count == 0 {
		return 0
	}
	return s.maxV
}

func (s *Samples) Avg() float64 {
	if s.count == 0 {
		return 0
	}
	return float64(s.sum) / float64(s.count)
}

// bucketIndex maps v to its histogram slot: slot 0 holds zero (and any
// non-positive value), slot k>=1 covers [10^((k-1)/bucketsPerDecade),
// 10^(k/bucketsPerDecade)); values at or above 10^maxDecade clamp to the
// last slot.
func bucketIndex(v int64) int {
	if v <= 0 {
		return 0
	}
	idx := int(math.Log10(float64(v))*bucketsPerDecade) + 1
	if idx >= bucketCount {
		idx = bucketCount - 1
	}
	return idx
}

// bucketValue is the geometric middle of the bucket: the representative
// value reported for percentiles that fall inside it.
func bucketValue(idx int) float64 {
	if idx <= 0 {
		return 0
	}
	return math.Pow(10, (float64(idx)-0.5)/bucketsPerDecade)
}

// Percentile returns the p-th percentile (0..100, clamped) by the nearest-rank
// method over the histogram; with no observations it reports 0.
func (s *Samples) Percentile(p float64) float64 {
	if s.count == 0 {
		return 0
	}
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	rank := int64(math.Ceil(p / 100 * float64(s.count)))
	if rank < 1 {
		rank = 1
	}
	var cum int64
	for idx, c := range s.buckets {
		cum += c
		if rank <= cum {
			return bucketValue(idx)
		}
	}
	return float64(s.maxV)
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
