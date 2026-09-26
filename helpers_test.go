package main

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("could not parse url %q: %s", raw, err)
	}
	return parsed
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	return writeTempFileNamed(t, "requests.json", content)
}

func writeTempFileNamed(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("could not write temp file: %s", err)
	}
	return path
}

// captureStdout swaps os.Stdout for a pipe while fn runs and returns
// everything printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("could not create pipe: %s", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()
	w.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("could not read captured output: %s", err)
	}
	return string(data)
}

// setLoadGlobals overrides package-level configuration for a test
// and restores the previous values when the test finishes.
func setLoadGlobals(t *testing.T, load string, c int, requestTimeout time.Duration) {
	t.Helper()
	oldConcurrency, oldTimeout, oldLoadType := concurrency, timeout, loadType
	concurrency, timeout, loadType = c, requestTimeout, load
	t.Cleanup(func() {
		concurrency, timeout, loadType = oldConcurrency, oldTimeout, oldLoadType
	})
}

// setHeaders overrides the parsed -H flags for a test and restores the
// previous value when the test finishes.
func setHeaders(t *testing.T, headers [][2]string) {
	t.Helper()
	oldHeaders := cliHeaders
	cliHeaders = headers
	t.Cleanup(func() {
		cliHeaders = oldHeaders
	})
}

func newTestRunner(t *testing.T) *HttpRunner {
	t.Helper()
	wgReady := &sync.WaitGroup{}
	wgReady.Add(1)
	runner := NewHttpRunner(0, wgReady)
	wgReady.Wait()
	return runner
}
