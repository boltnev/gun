package main

import (
	"context"
	"errors"
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
