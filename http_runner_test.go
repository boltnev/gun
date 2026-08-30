package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestHttpRunnerSendsRequestAndCollectsResult(t *testing.T) {
	type serverCall struct {
		method string
		path   string
		body   string
	}
	var mu sync.Mutex
	var calls []serverCall

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("server could not read body: %s", err)
		}
		mu.Lock()
		calls = append(calls, serverCall{method: r.Method, path: r.URL.Path, body: string(body)})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	setLoadGlobals(t, LoadTypeHTTP, 1, 5*time.Second)

	requests := make(chan *Request, 1)
	results := make(chan Result, 1)

	reqURL := mustParseURL(t, srv.URL)
	reqURL.Path = "/api/echo"
	requests <- &Request{Url: reqURL, Method: http.MethodPost, Body: `{"value":42}`}
	close(requests)

	runner := newTestRunner(t)
	wgDone := &sync.WaitGroup{}
	wgDone.Add(1)
	runner.Run(context.Background(), wgDone, requests, results)
	wgDone.Wait()

	res := <-results
	if res.err != nil {
		t.Errorf("unexpected error: %s", res.err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want %d", res.StatusCode, http.StatusOK)
	}
	if res.Latency < 0 {
		t.Errorf("latency = %s, want non-negative", res.Latency)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("server calls = %d, want 1", len(calls))
	}
	want := serverCall{method: http.MethodPost, path: "/api/echo", body: `{"value":42}`}
	if calls[0] != want {
		t.Errorf("server saw %+v, want %+v", calls[0], want)
	}
}

func TestHttpRunnerReportsNon2xxStatusWithoutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	setLoadGlobals(t, LoadTypeHTTP, 1, 5*time.Second)

	requests := make(chan *Request, 1)
	results := make(chan Result, 1)
	requests <- &Request{Url: mustParseURL(t, srv.URL), Method: http.MethodGet}
	close(requests)

	runner := newTestRunner(t)
	wgDone := &sync.WaitGroup{}
	wgDone.Add(1)
	runner.Run(context.Background(), wgDone, requests, results)
	wgDone.Wait()

	res := <-results
	if res.err != nil {
		t.Errorf("unexpected error: %s", res.err)
	}
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", res.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHttpRunnerReportsConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	setLoadGlobals(t, LoadTypeHTTP, 1, 5*time.Second)

	requests := make(chan *Request, 1)
	results := make(chan Result, 1)
	requests <- &Request{Url: mustParseURL(t, srv.URL), Method: http.MethodGet}
	close(requests)

	runner := newTestRunner(t)
	wgDone := &sync.WaitGroup{}
	wgDone.Add(1)
	runner.Run(context.Background(), wgDone, requests, results)
	wgDone.Wait()

	res := <-results
	if res.err == nil {
		t.Error("expected connection error, got nil")
	}
	if res.StatusCode != 0 {
		t.Errorf("status code = %d, want 0 on transport error", res.StatusCode)
	}
}

func TestHttpRunnerRespectsRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	setLoadGlobals(t, LoadTypeHTTP, 1, 30*time.Millisecond)

	requests := make(chan *Request, 1)
	results := make(chan Result, 1)
	requests <- &Request{Url: mustParseURL(t, srv.URL), Method: http.MethodGet}
	close(requests)

	runner := newTestRunner(t)
	wgDone := &sync.WaitGroup{}
	wgDone.Add(1)
	runner.Run(context.Background(), wgDone, requests, results)
	wgDone.Wait()

	res := <-results
	if !errors.Is(res.err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want context.DeadlineExceeded", res.err)
	}
	if res.StatusCode != 0 {
		t.Errorf("status code = %d, want 0 on timeout", res.StatusCode)
	}
}

func TestHttpRunnerSendsDefaultUserAgent(t *testing.T) {
	var mu sync.Mutex
	var userAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		userAgent = r.Header.Get("User-Agent")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	setLoadGlobals(t, LoadTypeHTTP, 1, 5*time.Second)

	requests := make(chan *Request, 1)
	results := make(chan Result, 1)
	requests <- &Request{Url: mustParseURL(t, srv.URL), Method: http.MethodGet}
	close(requests)

	runner := newTestRunner(t)
	wgDone := &sync.WaitGroup{}
	wgDone.Add(1)
	runner.Run(context.Background(), wgDone, requests, results)
	wgDone.Wait()

	if res := <-results; res.err != nil {
		t.Errorf("unexpected error: %s", res.err)
	}
	mu.Lock()
	defer mu.Unlock()
	if userAgent != DefaultUserAgent {
		t.Errorf("user agent = %q, want %q", userAgent, DefaultUserAgent)
	}
}

func TestHttpRunnerAppliesCliAndRequestHeaders(t *testing.T) {
	setHeaders(t, [][2]string{
		{"X-Cli", "cli"},
		{"User-Agent", "cli-agent"},
	})

	var mu sync.Mutex
	seen := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen["X-Cli"] = r.Header.Get("X-Cli")
		seen["X-Req"] = r.Header.Get("X-Req")
		seen["User-Agent"] = r.Header.Get("User-Agent")
		seen["Host"] = r.Host
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	setLoadGlobals(t, LoadTypeHTTP, 1, 5*time.Second)

	requests := make(chan *Request, 1)
	results := make(chan Result, 1)
	requests <- &Request{
		Url:    mustParseURL(t, srv.URL),
		Method: http.MethodGet,
		Headers: map[string]string{
			"X-Req":      "req",
			"User-Agent": "req-agent",
			"Host":       "example.test",
		},
	}
	close(requests)

	runner := newTestRunner(t)
	wgDone := &sync.WaitGroup{}
	wgDone.Add(1)
	runner.Run(context.Background(), wgDone, requests, results)
	wgDone.Wait()

	if res := <-results; res.err != nil {
		t.Errorf("unexpected error: %s", res.err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := map[string]string{
		"X-Cli":      "cli",
		"X-Req":      "req",
		"User-Agent": "req-agent", // per-request headers override -H
		"Host":       "example.test",
	}
	for name, wantValue := range want {
		if seen[name] != wantValue {
			t.Errorf("%s = %q, want %q", name, seen[name], wantValue)
		}
	}
}

func TestHttpRunnerProcessesAllRequestsFromChannel(t *testing.T) {
	var mu sync.Mutex
	pathsServed := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		pathsServed++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	setLoadGlobals(t, LoadTypeHTTP, 1, 5*time.Second)

	const requestCount = 3
	requests := make(chan *Request, requestCount)
	results := make(chan Result, requestCount)
	for i := range requestCount {
		reqURL := mustParseURL(t, srv.URL)
		reqURL.Path = fmt.Sprintf("/req/%d", i)
		requests <- &Request{Url: reqURL, Method: http.MethodGet}
	}
	close(requests)

	runner := newTestRunner(t)
	wgDone := &sync.WaitGroup{}
	wgDone.Add(1)
	runner.Run(context.Background(), wgDone, requests, results)
	wgDone.Wait()

	for range requestCount {
		res := <-results
		if res.err != nil {
			t.Errorf("unexpected error: %s", res.err)
		}
		if res.StatusCode != http.StatusOK {
			t.Errorf("status code = %d, want %d", res.StatusCode, http.StatusOK)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if pathsServed != requestCount {
		t.Errorf("server served %d requests, want %d", pathsServed, requestCount)
	}
}
