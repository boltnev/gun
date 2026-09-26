package main

import (
	"math"
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

func TestSamplesPercentileInterpolates(t *testing.T) {
	var s Samples
	for i := 1; i <= 10; i++ {
		s.Observe(int64(time.Duration(i) * time.Millisecond))
	}

	cases := []struct {
		p    float64
		want float64 // in milliseconds
	}{
		{0, 1},
		{25, 3.25},
		{50, 5.5},
		{75, 7.75},
		{90, 9.1},
		{95, 9.55},
		{99, 9.91},
		{99.9, 9.991},
		{100, 10},
	}
	for _, c := range cases {
		got := s.Percentile(c.p) / float64(time.Millisecond)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("percentile(%v) = %vms, want %vms", c.p, got, c.want)
		}
	}

	if s.Percentile(-5) != s.Percentile(0) {
		t.Error("negative percentile is not clamped to p0")
	}
	if s.Percentile(200) != s.Percentile(100) {
		t.Error("percentile above 100 is not clamped to p100")
	}
}

func TestSamplesSingleValue(t *testing.T) {
	var s Samples
	s.Observe(int64(7 * time.Millisecond))

	want := float64(7 * time.Millisecond)
	for _, p := range []float64{0, 50, 99.9, 100} {
		if got := s.Percentile(p); got != want {
			t.Errorf("percentile(%v) = %f, want %f", p, got, want)
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

func TestSamplesObserveAfterQueryResorts(t *testing.T) {
	var s Samples
	s.Observe(int64(30 * time.Millisecond))
	if s.Max() != int64(30*time.Millisecond) {
		t.Errorf("max = %s, want 30ms", time.Duration(s.Max()))
	}

	s.Observe(int64(10 * time.Millisecond))
	if s.Min() != int64(10*time.Millisecond) {
		t.Errorf("min after new sample = %s, want 10ms", time.Duration(s.Min()))
	}
}

func TestDurationStatsLine(t *testing.T) {
	var s Samples
	for _, d := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond} {
		s.Observe(int64(d))
	}

	want := "min 10ms; avg 20ms; max 30ms; p50 20ms; p75 25ms; p90 28ms; p95 29ms; p99 29.8ms; p99.9 29.98ms"
	if got := DurationStatsLine(&s); got != want {
		t.Errorf("duration stats line = %q, want %q", got, want)
	}
}

func TestSizeStatsLine(t *testing.T) {
	var s Samples
	for _, v := range []int64{100, 200, 300} {
		s.Observe(v)
	}

	want := "min 100; avg 200; max 300; p50 200; p75 250; p90 280; p95 290; p99 298; p99.9 300"
	if got := SizeStatsLine(&s); got != want {
		t.Errorf("size stats line = %q, want %q", got, want)
	}
}
