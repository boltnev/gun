package main

import (
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
	path := filepath.Join(t.TempDir(), "requests.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("could not write temp file: %s", err)
	}
	return path
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

func newTestRunner(t *testing.T) *HttpRunner {
	t.Helper()
	wgReady := &sync.WaitGroup{}
	wgReady.Add(1)
	runner := NewHttpRunner(0, wgReady)
	wgReady.Wait()
	return runner
}
