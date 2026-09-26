package main

import (
	"strings"
	"testing"
	"time"
)

func TestStartSectionLines(t *testing.T) {
	lines := startSectionLines(startSpec{
		LoadType:    "http",
		Url:         "http://127.0.0.1:8137/",
		Concurrency: 4,
		Duration:    10 * time.Second,
		Timeout:     10 * time.Second,
		Rate:        100,
		Burst:       100,
		Requests:    "2 prepared from /tmp/requests.json",
		Started:     time.Date(2026, 9, 26, 17, 5, 1, 0, time.UTC),
	})

	want := []string{
		"gun — http load test",
		"  url          http://127.0.0.1:8137/",
		"  concurrency  4",
		"  duration     10s",
		"  timeout      10s",
		"  rate         100 rps (burst 100)",
		"  requests     2 prepared from /tmp/requests.json",
		"  started      2026-09-26 17:05:01",
		separator,
	}
	if len(lines) != len(want) {
		t.Fatalf("start section has %d lines, want %d:\n%s", len(lines), len(want), strings.Join(lines, "\n"))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestStartSectionWithoutRateOmitsLine(t *testing.T) {
	lines := startSectionLines(startSpec{LoadType: "qdrant", Concurrency: 1})
	for _, line := range lines {
		if strings.Contains(line, "rate") {
			t.Errorf("rate line must be omitted without -rate, got %q", line)
		}
	}
}

func TestStatsTableLines(t *testing.T) {
	var lat, ttfb, size Samples
	for _, d := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond} {
		lat.Observe(int64(d))
	}
	for _, d := range []time.Duration{5 * time.Millisecond, 6 * time.Millisecond, 7 * time.Millisecond} {
		ttfb.Observe(int64(d))
	}
	for _, v := range []int64{100, 200, 300} {
		size.Observe(v)
	}

	lines := statsTableLines(&lat, &ttfb, &size)
	if len(lines) != 10 { // header row + 9 stat rows
		t.Fatalf("table has %d lines, want 10:\n%s", len(lines), strings.Join(lines, "\n"))
	}

	wantHeader := "         time-to-response time-to-first-byte full-response-size"
	if lines[0] != wantHeader {
		t.Errorf("header = %q, want %q", lines[0], wantHeader)
	}

	pad := func(s string, w int) string {
		for len([]rune(s)) < w {
			s = " " + s
		}
		return s
	}
	// min/max/avg are exact running values, so the rows are deterministic
	wantRows := map[string]string{
		"min": "  min    " + pad("10.00ms", colDuration) + " " + pad("5.00ms", colTtfb) + " " + pad("100B", colSize),
		"max": "  max    " + pad("30.00ms", colDuration) + " " + pad("7.00ms", colTtfb) + " " + pad("300B", colSize),
		"avg": "  avg    " + pad("20.00ms", colDuration) + " " + pad("6.00ms", colTtfb) + " " + pad("200B", colSize),
	}
	rowsByLabel := map[string]string{}
	for _, line := range lines[1:] {
		rowsByLabel[strings.TrimSpace(line[:8])] = line
	}
	for label, want := range wantRows {
		if rowsByLabel[label] != want {
			t.Errorf("%s row = %q, want %q", label, rowsByLabel[label], want)
		}
	}

	// row order: min, max, avg, then percentiles ascending
	order := []string{"min", "max", "avg", "p50", "p75", "p90", "p95", "p99", "p99.9"}
	prev := -1
	for _, label := range order {
		idx := strings.Index(strings.Join(lines, "\n"), "\n  "+label+" ")
		if idx < prev {
			t.Errorf("row %s is out of order", label)
		}
		prev = idx
	}
}

func TestStatsTableSingleColumnWithoutHttpMetrics(t *testing.T) {
	var lat Samples
	lat.Observe(int64(10 * time.Millisecond))

	lines := statsTableLines(&lat, &Samples{}, &Samples{})

	if len(lines) != 10 {
		t.Fatalf("table has %d lines, want 10:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[0], "time-to-response") {
		t.Errorf("header %q must name time-to-response", lines[0])
	}
	for _, forbidden := range []string{"time-to-first-byte", "full-response-size"} {
		if strings.Contains(strings.Join(lines, "\n"), forbidden) {
			t.Errorf("single-column table must not mention %s", forbidden)
		}
	}
}

func TestStatsTableNoSamples(t *testing.T) {
	if lines := statsTableLines(&Samples{}, &Samples{}, &Samples{}); lines != nil {
		t.Errorf("table without samples = %v, want none", lines)
	}
}

func TestFormatCount(t *testing.T) {
	cases := map[int]string{
		0:       "0",
		999:     "999",
		1204:    "1 204",
		1000000: "1 000 000",
	}
	for n, want := range cases {
		if got := formatCount(n); got != want {
			t.Errorf("formatCount(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFormatNsTwoDecimalsAdaptiveUnit(t *testing.T) {
	cases := map[float64]string{
		689:                  "689.00ns",
		79061:                "79.06µs",
		311641:               "311.64µs",
		178089075:            "178.09ms",
		500000000:            "500.00ms",
		1002500000:           "1.00s",
		65300000000:          "65.30s",
		float64(time.Second): "1.00s",
	}
	for v, want := range cases {
		if got := formatNs(v); got != want {
			t.Errorf("formatNs(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestProgressBar(t *testing.T) {
	cases := []struct {
		elapsed time.Duration
		total   time.Duration
		want    string
	}{
		{0, 10 * time.Second, "[" + strings.Repeat("░", 24) + "]   0%"},
		{5 * time.Second, 10 * time.Second, "[" + strings.Repeat("█", 12) + strings.Repeat("░", 12) + "]  50%"},
		{10 * time.Second, 10 * time.Second, "[" + strings.Repeat("█", 24) + "] 100%"},
		{15 * time.Second, 10 * time.Second, "[" + strings.Repeat("█", 24) + "] 100%"}, // clamped
		{3 * time.Second, 0, "[" + strings.Repeat("░", 24) + "]   0%"},                 // no duration
	}
	for _, c := range cases {
		if got := progressBar(c.elapsed, c.total); got != c.want {
			t.Errorf("progressBar(%s, %s) = %q, want %q", c.elapsed, c.total, got, c.want)
		}
	}
}

func TestPadLeftCountsRunesNotBytes(t *testing.T) {
	// µ is 2 bytes but 1 cell: padding by bytes would make the column short
	if got, want := padLeft("µs", 4), "  µs"; got != want {
		t.Errorf("padLeft(%q, 4) = %q, want %q", "µs", got, want)
	}
	if got, want := padLeft("178.09ms", 16), "        178.09ms"; got != want {
		t.Errorf("padLeft ascii = %q, want %q", got, want)
	}
	if got := padLeft("already longer than width", 4); got != "already longer than width" {
		t.Errorf("padLeft overlong = %q, want unchanged", got)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[float64]string{
		0:           "0B",
		529:         "529B",
		1023:        "1023B",
		1024:        "1KB",
		1536:        "1.5KB",
		1580:        "1.54KB",
		842752:      "823KB",
		5 * 1 << 20: "5MB",
	}
	for v, want := range cases {
		if got := humanBytes(v); got != want {
			t.Errorf("humanBytes(%v) = %q, want %q", v, got, want)
		}
	}
}
