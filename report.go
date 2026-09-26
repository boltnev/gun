package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The report is laid out in a fixed 63-column grid: the metrics table is
// 2 indent + 6 label + 1 + 16 + 1 + 18 + 1 + 18, and the section rules match.
const sectionWidth = 63

const (
	colDuration = 16 // "time-to-response"
	colTtfb     = 18 // "time-to-first-byte"
	colSize     = 18 // "full-response-size"
)

var separator = strings.Repeat("─", sectionWidth)

// kv renders an indented "label   value" line; the 13-char label column is
// shared by the start, progress, and finish sections.
func kv(label, value string) string {
	return fmt.Sprintf("  %-13s%s", label, value)
}

// startSpec is everything the start section shows; main fills it from the
// parsed flags.
type startSpec struct {
	LoadType    string
	Url         string
	Concurrency int
	Duration    time.Duration
	Timeout     time.Duration
	Rate        float64
	Burst       int
	Requests    string
	Started     time.Time
}

func startSectionLines(s startSpec) []string {
	lines := []string{
		fmt.Sprintf("gun — %s load test", s.LoadType),
		kv("url", s.Url),
		kv("concurrency", strconv.Itoa(s.Concurrency)),
		kv("duration", s.Duration.String()),
		kv("timeout", s.Timeout.String()),
	}
	if s.Rate > 0 {
		lines = append(lines, kv("rate", fmt.Sprintf("%.0f rps (burst %d)", s.Rate, s.Burst)))
	}
	lines = append(lines,
		kv("requests", s.Requests),
		kv("started", s.Started.Format("2006-01-02 15:04:05")),
		separator,
	)
	return lines
}

// tableRows is the fixed row set of the metrics table: min, max, avg, then
// percentiles.
var tableRows = []struct {
	label string
	get   func(s *Samples) float64
}{
	{"min", func(s *Samples) float64 { return float64(s.Min()) }},
	{"max", func(s *Samples) float64 { return float64(s.Max()) }},
	{"avg", (*Samples).Avg},
	{"p50", func(s *Samples) float64 { return s.Percentile(50) }},
	{"p75", func(s *Samples) float64 { return s.Percentile(75) }},
	{"p90", func(s *Samples) float64 { return s.Percentile(90) }},
	{"p95", func(s *Samples) float64 { return s.Percentile(95) }},
	{"p99", func(s *Samples) float64 { return s.Percentile(99) }},
	{"p99.9", func(s *Samples) float64 { return s.Percentile(99.9) }},
}

type tableColumn struct {
	header  string
	width   int
	format  func(float64) string
	samples *Samples
}

// statsTableLines renders the metrics table shared by the progress and
// finish sections: one column per metric with samples (time-to-first-byte
// and full-response-size exist in http mode only), rows in the fixed
// min/max/avg/percentiles order. Returns no lines without latency samples.
func statsTableLines(lat, ttfb, size *Samples) []string {
	if lat.Count() == 0 {
		return nil
	}
	cols := []tableColumn{{"time-to-response", colDuration, formatNs, lat}}
	if ttfb.Count() > 0 {
		cols = append(cols, tableColumn{"time-to-first-byte", colTtfb, formatNs, ttfb})
	}
	if size.Count() > 0 {
		cols = append(cols, tableColumn{"full-response-size", colSize, humanBytes, size})
	}

	cells := make([]string, len(cols))
	for i, col := range cols {
		cells[i] = padLeft(col.header, col.width)
	}
	lines := []string{fmt.Sprintf("  %-6s %s", "", strings.Join(cells, " "))}
	for _, row := range tableRows {
		for i, col := range cols {
			cells[i] = padLeft(col.format(row.get(col.samples)), col.width)
		}
		lines = append(lines, fmt.Sprintf("  %-6s %s", row.label, strings.Join(cells, " ")))
	}
	return lines
}

// formatNs renders a duration with two decimals in an adaptive unit
// (ns/µs/ms/s): 689.00ns, 79.06µs, 178.09ms, 1.00s.
func formatNs(v float64) string {
	val, unit := v, "ns"
	switch {
	case v >= float64(time.Second):
		val, unit = v/float64(time.Second), "s"
	case v >= float64(time.Millisecond):
		val, unit = v/float64(time.Millisecond), "ms"
	case v >= float64(time.Microsecond):
		val, unit = v/float64(time.Microsecond), "µs"
	}
	return fmt.Sprintf("%.2f%s", val, unit)
}

// humanBytes renders a byte count as B/KB/MB/GB with up to two decimals and
// trimmed trailing zeros: 529B, 1.5KB, 1.54KB, 823KB.
func humanBytes(v float64) string {
	unit, val := "B", v
	for _, u := range []string{"KB", "MB", "GB", "TB"} {
		if val < 1024 {
			break
		}
		val /= 1024
		unit = u
	}
	if unit == "B" {
		return strconv.Itoa(int(math.Round(val))) + unit
	}
	s := strconv.FormatFloat(val, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s + unit
}

// formatCount groups digits with spaces: 1 204, 1 234 567.
func formatCount(n int) string {
	s := strconv.Itoa(n)
	var b strings.Builder
	for i, digit := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(digit)
	}
	return b.String()
}

// progressBarWidth keeps the running line inside the 63-column grid.
const progressBarWidth = 24

// progressBar renders a fixed-width bar for the share of the test duration
// already elapsed, like [████████░░░░░░░░░░░░░░░░░░░░] 42%; it clamps at both
// ends, so an interrupted run settles at 100%.
func progressBar(elapsed, total time.Duration) string {
	frac := 0.0
	if total > 0 {
		frac = float64(elapsed) / float64(total)
		if frac > 1 {
			frac = 1
		}
		if frac < 0 {
			frac = 0
		}
	}
	filled := int(math.Round(frac * progressBarWidth))
	return fmt.Sprintf("[%s%s] %3.0f%%",
		strings.Repeat("█", filled),
		strings.Repeat("░", progressBarWidth-filled),
		frac*100,
	)
}

// padLeft right-aligns s in width display cells. Cell count is rune count:
// the report mixes ASCII with µ and ─, which are multi-byte but single-width.
func padLeft(s string, width int) string {
	for utf8.RuneCountInString(s) < width {
		s = " " + s
	}
	return s
}
