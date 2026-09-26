package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

// Collect prints stats instead of returning them, so per load type
// the observable contract is: it drains the results channel, survives both
// successful and failed results, and releases the WaitGroup.
func TestSimpleCollectorHttpDrainsResultsAndReleasesWaitGroup(t *testing.T) {
	setLoadGlobals(t, LoadTypeHTTP, 1, time.Second)

	results := make(chan Result, 3)
	results <- Result{Latency: 10 * time.Millisecond, StatusCode: 200}
	results <- Result{Latency: 20 * time.Millisecond, StatusCode: 500, err: errors.New("boom")}
	results <- Result{Latency: 5 * time.Millisecond, StatusCode: 0, err: errors.New("connection refused")}
	close(results)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	NewSimpleCollector().Collect(context.Background(), wg, results)

	waited := make(chan struct{})
	go func() {
		wg.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("Collect did not call wg.Done()")
	}
}

// The final report has a percentile block per metric over successful
// responses only; errored results are counted but left out of the samples.
func TestSimpleCollectorHttpPrintsLatencyStats(t *testing.T) {
	setLoadGlobals(t, LoadTypeHTTP, 1, time.Second)

	results := make(chan Result, 4)
	results <- Result{Latency: 10 * time.Millisecond, FirstByteLatency: 5 * time.Millisecond, SizeBytes: 100, StatusCode: 200}
	results <- Result{Latency: 20 * time.Millisecond, FirstByteLatency: 6 * time.Millisecond, SizeBytes: 200, StatusCode: 200}
	results <- Result{Latency: 30 * time.Millisecond, FirstByteLatency: 7 * time.Millisecond, SizeBytes: 300, StatusCode: 200}
	results <- Result{Latency: 40 * time.Millisecond, StatusCode: 0, err: errors.New("connection refused")}
	close(results)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	out := captureStdout(t, func() {
		NewSimpleCollector().Collect(context.Background(), wg, results)
	})
	wg.Wait()

	for _, want := range []string{
		separator,
		"  finished    ",
		"  responses    4   rps ",
		"  statuses     0=1, 200=3",
		"time-to-response time-to-first-byte full-response-size",
		"  sent         0B total   0B/s",
		"  recv         600B total   ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("collector output missing %q\ngot:\n%s", want, out)
		}
	}
	if strings.Contains(out, "running") {
		t.Error("mid-run progress must not print without a terminal")
	}
}

// The ticker block carries the live status counts: the second result arrives
// after the 500ms tick, so the progress redraw must already include it.
func TestSimpleCollectorHttpCountsStatusesMidRun(t *testing.T) {
	setLoadGlobals(t, LoadTypeHTTP, 1, time.Second)

	results := make(chan Result, 2)
	c := NewSimpleCollector()
	c.interactive = true // force the redraw path under the test pipe
	out := captureStdout(t, func() {
		wg := &sync.WaitGroup{}
		wg.Add(1)
		go c.Collect(context.Background(), wg, results)

		results <- Result{Latency: 10 * time.Millisecond, FirstByteLatency: 5 * time.Millisecond, SizeBytes: 100, StatusCode: 200}
		time.Sleep(600 * time.Millisecond) // let the 500ms ticker fire
		results <- Result{Latency: 20 * time.Millisecond, FirstByteLatency: 6 * time.Millisecond, SizeBytes: 200, StatusCode: 500}
		close(results)
		wg.Wait()
	})

	if !strings.Contains(out, "200=1, 500=1") {
		t.Errorf("mid-run output missing live status counts:\n%s", out)
	}
	if !strings.Contains(out, "░") {
		t.Errorf("mid-run output missing the progress bar:\n%s", out)
	}
	if !strings.Contains(out, "transfer") {
		t.Errorf("mid-run output missing live transfer speeds:\n%s", out)
	}
	if !strings.Contains(out, "\x1b[2K") {
		t.Error("interactive redraw must use ANSI codes")
	}
}

func TestStatusCountsSortedAndDeterministic(t *testing.T) {
	got := statusCounts(map[int]int{500: 1, 200: 2, 0: 3})
	if want := "0=3, 200=2, 500=1"; got != want {
		t.Errorf("statusCounts = %q, want %q", got, want)
	}
}

func TestStatusCountsEmpty(t *testing.T) {
	if got := statusCounts(map[int]int{}); got != "(none)" {
		t.Errorf("statusCounts on empty map = %q, want %q", got, "(none)")
	}
}

// Qdrant mode has latency samples only: the runner records neither time to
// first byte nor response size, so those blocks are skipped entirely.
func TestSimpleCollectorQdrantPrintsLatencyStatsOnly(t *testing.T) {
	setLoadGlobals(t, LoadTypeQdrant, 1, time.Second)

	results := make(chan Result, 2)
	results <- Result{Latency: 10 * time.Millisecond, AnyData: []*qdrant.ScoredPoint{{Score: 0.9}}}
	results <- Result{Latency: 30 * time.Millisecond, AnyData: []*qdrant.ScoredPoint{{Score: 0.5}}}
	close(results)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	out := captureStdout(t, func() {
		NewSimpleCollector().Collect(context.Background(), wg, results)
	})
	wg.Wait()

	for _, want := range []string{
		"  points avg   ",
		"time-to-response",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("collector output missing %q\ngot:\n%s", want, out)
		}
	}
	if strings.Contains(out, "time-to-first-byte") || strings.Contains(out, "full-response-size") {
		t.Errorf("qdrant output must not contain http-only metric columns:\n%s", out)
	}
}

// A line wider than the terminal must be clipped, not wrapped: wrapping
// would occupy a second physical row and break the redraw arithmetic.
func TestTruncateLine(t *testing.T) {
	cases := []struct {
		line  string
		width int
		want  string
	}{
		{"  running    6.2s", 0, "  running    6.2s"}, // unknown width: unchanged
		{"short", 80, "short"},
		{"  running    6.2s   responses 1 204", 20, "  running    6.2s   "},
		{"─" + strings.Repeat("─", 40), 10, strings.Repeat("─", 10)}, // rune-safe, not byte-safe
	}
	for _, c := range cases {
		if got := truncateLine(c.line, c.width); got != c.want {
			t.Errorf("truncateLine(%q, %d) = %q, want %q", c.line, c.width, got, c.want)
		}
	}
}

// In interactive mode redraw moves the cursor up over the previous block,
// clears each line, and shrinks leftovers; clearProgress erases the block.
func TestSimpleCollectorRedrawReplacesLinesInPlace(t *testing.T) {
	s := &SimpleCollector{interactive: true}
	out := captureStdout(t, func() {
		s.redraw([]string{"one", "two"})
		s.redraw([]string{"three"})
		s.clearProgress()
	})

	want := "\x1b[2Kone\n\x1b[2Ktwo\n\x1b[J" + // first draw: two cleared lines, erase below
		"\x1b[2A\x1b[2Kthree\n\x1b[J" + // redraw: up two lines, one line, erase the old second line
		"\x1b[1A\x1b[J" // clear: up one line, erase the block
	if out != want {
		t.Errorf("redraw output = %q, want %q", out, want)
	}
}

// Without a terminal the progress block is skipped entirely: logs and pipes
// only ever see the start and finish sections.
func TestSimpleCollectorRedrawPlainWithoutTerminal(t *testing.T) {
	s := &SimpleCollector{interactive: false}
	out := captureStdout(t, func() {
		s.redraw([]string{"one", "two"})
		s.redraw([]string{"three"})
		s.clearProgress()
	})

	if out != "" {
		t.Errorf("plain redraw output = %q, want empty", out)
	}
}

func TestSimpleCollectorQdrantDrainsResultsAndReleasesWaitGroup(t *testing.T) {
	setLoadGlobals(t, LoadTypeQdrant, 1, time.Second)

	results := make(chan Result, 2)
	results <- Result{
		Latency: 10 * time.Millisecond,
		AnyData: []*qdrant.ScoredPoint{{Score: 0.9}, {Score: 0.1}},
	}
	results <- Result{Latency: 5 * time.Millisecond, err: errors.New("boom")}
	close(results)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	NewSimpleCollector().Collect(context.Background(), wg, results)

	waited := make(chan struct{})
	go func() {
		wg.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("Collect did not call wg.Done()")
	}
}
