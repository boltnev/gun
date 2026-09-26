package main

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestSamplesMinAvgMax(t *testing.T) {
	var s Samples
	for _, d := range []time.Duration{10 * time.Millisecond, 30 * time.Millisecond, 20 * time.Millisecond} {
		s.Observe(int64(d))
	}

	if got := s.Count(); got != 3 {
		t.Errorf("count = %d, want 3", got)
	}
	if got := s.Min(); got != int64(10*time.Millisecond) {
		t.Errorf("min = %s, want 10ms", time.Duration(got))
	}
	if got := s.Max(); got != int64(30*time.Millisecond) {
		t.Errorf("max = %s, want 30ms", time.Duration(got))
	}
	if got := s.Avg(); got != float64(20*time.Millisecond) {
		t.Errorf("avg = %fns, want 20000000ns", got)
	}
}

// Percentiles are nearest-rank over the histogram, so for 1..10ms the
// expected value is the rank-th sample itself; the bucket representative may
// deviate by half a bucket width (~0.12%).
func TestSamplesPercentileNearestRank(t *testing.T) {
	var s Samples
	for i := 1; i <= 10; i++ {
		s.Observe(int64(time.Duration(i) * time.Millisecond))
	}

	cases := []struct {
		p    float64
		want float64 // in milliseconds
	}{
		{0, 1},
		{25, 3},
		{50, 5},
		{75, 8},
		{90, 9},
		{95, 10},
		{99, 10},
		{99.9, 10},
		{100, 10},
	}
	for _, c := range cases {
		got := s.Percentile(c.p) / float64(time.Millisecond)
		if math.Abs(got-c.want) > c.want*0.005 {
			t.Errorf("percentile(%v) = %vms, want %vms ±0.5%%", c.p, got, c.want)
		}
	}

	if s.Percentile(-5) != s.Percentile(0) {
		t.Error("negative percentile is not clamped to p0")
	}
	if s.Percentile(200) != s.Percentile(100) {
		t.Error("percentile above 100 is not clamped to p100")
	}
}

// With enough samples the nearest-rank answer is tight: on 1..1000ms the
// p50/p99/p99.9 ranks are 500/990/999, and the bucket representative must
// stay within half a percent of those.
func TestSamplesPercentileOnLargeSample(t *testing.T) {
	var s Samples
	for i := 1; i <= 1000; i++ {
		s.Observe(int64(time.Duration(i) * time.Millisecond))
	}

	cases := []struct {
		p    float64
		want float64 // in milliseconds
	}{
		{50, 500},
		{99, 990},
		{99.9, 999},
	}
	for _, c := range cases {
		got := s.Percentile(c.p) / float64(time.Millisecond)
		if math.Abs(got-c.want) > c.want*0.005 {
			t.Errorf("percentile(%v) = %vms, want %vms ±0.5%%", c.p, got, c.want)
		}
	}
}

func TestSamplesPercentilesAreMonotonic(t *testing.T) {
	var s Samples
	for i := 1; i <= 101; i++ {
		s.Observe(int64(i) * 7919)
	}

	prev := s.Percentile(0)
	for _, p := range []float64{10, 25, 50, 75, 90, 95, 99, 99.9, 100} {
		cur := s.Percentile(p)
		if cur < prev {
			t.Fatalf("percentile(%v) = %v, less than percentile below it (%v)", p, cur, prev)
		}
		prev = cur
	}
}

func TestSamplesSingleValue(t *testing.T) {
	var s Samples
	s.Observe(int64(7 * time.Millisecond))

	want := float64(7 * time.Millisecond)
	for _, p := range []float64{0, 50, 99.9, 100} {
		if got := s.Percentile(p); math.Abs(got-want) > want*0.005 {
			t.Errorf("percentile(%v) = %s, want %s ±0.5%%", p, time.Duration(got), time.Duration(want))
		}
	}
	if s.Min() != int64(want) || s.Max() != int64(want) || s.Avg() != want {
		t.Errorf("min/avg/max = %d/%f/%d, want %d for a single sample", s.Min(), s.Avg(), s.Max(), int64(want))
	}
}

func TestSamplesEmpty(t *testing.T) {
	var s Samples

	if s.Count() != 0 {
		t.Errorf("count = %d, want 0", s.Count())
	}
	if s.Min() != 0 || s.Max() != 0 || s.Avg() != 0 {
		t.Errorf("min/max/avg = %d/%d/%f, want zeros", s.Min(), s.Max(), s.Avg())
	}
	if s.Percentile(99) != 0 {
		t.Errorf("percentile(99) = %f, want 0", s.Percentile(99))
	}
}

// Zero goes to its own bucket, and values beyond the 10^13 range clamp to the
// top bucket instead of overflowing the index.
func TestSamplesZeroAndHugeValues(t *testing.T) {
	var s Samples
	s.Observe(0)
	s.Observe(1)
	s.Observe(int64(1e15))

	if got := s.Count(); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}
	if s.Min() != 0 {
		t.Errorf("min = %d, want 0", s.Min())
	}
	if s.Max() != int64(1e15) {
		t.Errorf("max = %d, want 1e15", s.Max())
	}
	// p50 is the 2nd of [0, 1, 1e15] → ~1
	if got := s.Percentile(50); math.Abs(got-1) > 0.01 {
		t.Errorf("percentile(50) = %f, want ~1", got)
	}
	// p100 is the 1e15 sample: the index clamps to the top bucket, whose
	// representative sits just under 10^13
	if got := s.Percentile(100); got < 9.9e12 || got > 1e13 {
		t.Errorf("percentile(100) = %f, want the top bucket just under 1e13", got)
	}
}

func TestDurationStatsLine(t *testing.T) {
	var s Samples
	for _, d := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond} {
		s.Observe(int64(d))
	}

	// min/avg/max are exact; percentile values are bucket representatives,
	// so only the shape of the line is asserted here
	got := DurationStatsLine(&s)
	if prefix := "min 10ms; avg 20ms; max 30ms; p50 "; !strings.HasPrefix(got, prefix) {
		t.Errorf("duration stats line = %q, want prefix %q", got, prefix)
	}
	for _, label := range []string{"; p75 ", "; p90 ", "; p95 ", "; p99 ", "; p99.9"} {
		if !strings.Contains(got, label) {
			t.Errorf("duration stats line = %q, missing %q", got, label)
		}
	}
}

func TestSizeStatsLine(t *testing.T) {
	var s Samples
	for _, v := range []int64{100, 200, 300} {
		s.Observe(v)
	}

	got := SizeStatsLine(&s)
	if prefix := "min 100; avg 200; max 300; p50 "; !strings.HasPrefix(got, prefix) {
		t.Errorf("size stats line = %q, want prefix %q", got, prefix)
	}
	for _, label := range []string{"; p75 ", "; p90 ", "; p95 ", "; p99 ", "; p99.9"} {
		if !strings.Contains(got, label) {
			t.Errorf("size stats line = %q, missing %q", got, label)
		}
	}
}
