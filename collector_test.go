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
		"responses total: 4\n",
		"latency stats (3 responses): min 10ms; avg 20ms; max 30ms; p50 20ms; p75 25ms; p90 28ms; p95 29ms; p99 29.8ms; p99.9 29.98ms\n",
		"time to first byte stats (3 responses): min 5ms; avg 6ms; max 7ms; p50 6ms; p75 6.5ms; p90 6.8ms; p95 6.9ms; p99 6.98ms; p99.9 6.998ms\n",
		"response size stats in bytes (3 responses): min 100; avg 200; max 300; p50 200; p75 250; p90 280; p95 290; p99 298; p99.9 300\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("collector output missing %q\ngot:\n%s", want, out)
		}
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

	want := "latency stats (2 responses): min 10ms; avg 20ms; max 30ms; p50 20ms; p75 25ms; p90 28ms; p95 29ms; p99 29.8ms; p99.9 29.98ms\n"
	if !strings.Contains(out, want) {
		t.Errorf("collector output missing %q\ngot:\n%s", want, out)
	}
	if strings.Contains(out, "time to first byte") || strings.Contains(out, "response size") {
		t.Errorf("qdrant output must not contain ttfb or size stats:\n%s", out)
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
