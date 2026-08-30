package main

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestNewFromJsonGeneratorErrorsOnBadSource(t *testing.T) {
	base := Request{Url: mustParseURL(t, "http://base.test/root"), Method: http.MethodGet}

	if _, err := NewFromJsonGenerator(base, "no_such_file.json"); err == nil {
		t.Error("expected error for missing file, got nil")
	}

	path := writeTempFile(t, `{not a json`)
	if _, err := NewFromJsonGenerator(base, path); err == nil {
		t.Error("expected error for invalid json, got nil")
	}
}

func TestNewFromJsonGeneratorAppliesUrlAndPath(t *testing.T) {
	baseURL := mustParseURL(t, "http://base.test/root")
	base := Request{Url: baseURL, Method: http.MethodGet}

	path := writeTempFile(t, `[
		{"method":"POST","path":"/from/path","body":"b"},
		{"url":"http://override.test/other"},
		{"url":"http://override.test/other","path":"/patched"},
		{"method":"GET"}
	]`)
	gen, err := NewFromJsonGenerator(base, path)
	if err != nil {
		t.Fatalf("could not create generator: %s", err)
	}
	reqs := gen.sourceRequests

	wantUrls := []string{
		"http://base.test/from/path",
		"http://override.test/other",
		"http://override.test/patched",
		"http://base.test/root",
	}
	for i, want := range wantUrls {
		if reqs[i].Url == nil || reqs[i].Url.String() != want {
			t.Errorf("request %d url = %v, want %s", i, reqs[i].Url, want)
		}
	}

	// every request must work on its own copy of the url, not on the shared base
	if reqs[0].Url == baseURL || reqs[3].Url == baseURL {
		t.Error("json requests must not share the base request url pointer")
	}
	if reqs[0].Url == reqs[3].Url {
		t.Error("each json request must get its own url copy")
	}
	reqs[0].Url.Path = "/mutated"
	if baseURL.Path != "/root" {
		t.Errorf("base url path mutated to %q, want %q", baseURL.Path, "/root")
	}
}

func TestFromJsonGeneratorLoadsExamplePayload(t *testing.T) {
	base := Request{Url: mustParseURL(t, "http://base.test"), Method: http.MethodGet}
	if _, err := NewFromJsonGenerator(base, "examples/payload.json"); err != nil {
		t.Errorf("could not load example payload: %s", err)
	}
}

func TestFromJsonGeneratorGenerateRequests(t *testing.T) {
	base := Request{Url: mustParseURL(t, "http://base.test"), Method: http.MethodGet}

	path := writeTempFile(t, `[
		{"method":"POST","path":"/one"},
		{"method":"POST","path":"/two"},
		{"method":"POST","path":"/three"}
	]`)
	gen, err := NewFromJsonGenerator(base, path)
	if err != nil {
		t.Fatalf("could not create generator: %s", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	requests := make(chan *Request)
	go gen.GenerateRequests(ctx, requests)

	allowedPaths := map[string]bool{"/one": true, "/two": true, "/three": true}
	seenPaths := map[string]bool{}
	const samples = 20
	for range samples {
		select {
		case req := <-requests:
			if req.Url == nil {
				t.Fatal("generated request without url")
			}
			if !allowedPaths[req.Url.Path] {
				t.Errorf("generated path %q, want one of /one, /two, /three", req.Url.Path)
			}
			if req.Method != http.MethodPost {
				t.Errorf("generated method = %q, want %q", req.Method, http.MethodPost)
			}
			seenPaths[req.Url.Path] = true
		case <-time.After(2 * time.Second):
			t.Fatal("generator stalled")
		}
	}
	if len(seenPaths) < 2 {
		t.Errorf("generator picked only %d distinct paths over %d samples, want random choice", len(seenPaths), samples)
	}

	// after cancel the generator may still win the select race and deliver a
	// few more requests; the guarantee is that the channel gets closed
	cancel()
	closedDeadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-requests:
			if !ok {
				return
			}
		case <-closedDeadline:
			t.Fatal("requests channel was not closed after cancel")
		}
	}
}
