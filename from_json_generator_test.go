package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
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

func TestNewFromJsonGeneratorParsesHeaders(t *testing.T) {
	base := Request{Url: mustParseURL(t, "http://base.test"), Method: http.MethodGet}

	path := writeTempFile(t, `[
		{"path":"/with/headers","headers":{"X-Token":"abc","User-Agent":"json-agent"}}
	]`)
	gen, err := NewFromJsonGenerator(base, path)
	if err != nil {
		t.Fatalf("could not create generator: %s", err)
	}

	headers := gen.sourceRequests[0].Headers
	if len(headers) != 2 {
		t.Fatalf("parsed %d headers, want 2: %v", len(headers), headers)
	}
	if headers["X-Token"] != "abc" {
		t.Errorf("X-Token = %q, want %q", headers["X-Token"], "abc")
	}
	if headers["User-Agent"] != "json-agent" {
		t.Errorf("User-Agent = %q, want %q", headers["User-Agent"], "json-agent")
	}
}

func TestFromJsonGeneratorLoadsExamplePayload(t *testing.T) {
	base := Request{Url: mustParseURL(t, "http://base.test"), Method: http.MethodGet}
	if _, err := NewFromJsonGenerator(base, "examples/payload.json"); err != nil {
		t.Errorf("could not load example payload: %s", err)
	}
}

func TestNewFromJsonGeneratorLoadsBodyFromFile(t *testing.T) {
	base := Request{Url: mustParseURL(t, "http://base.test"), Method: http.MethodGet}

	bodyPath := writeTempFileNamed(t, "body.json", `{"from":"file"}`)
	path := writeTempFile(t, fmt.Sprintf(`[
		{"method":"POST","path":"/one","body_file":%q}
	]`, bodyPath))
	gen, err := NewFromJsonGenerator(base, path)
	if err != nil {
		t.Fatalf("could not create generator: %s", err)
	}

	if body := gen.sourceRequests[0].Body; body != `{"from":"file"}` {
		t.Errorf("body = %q, want content of %q", body, bodyPath)
	}
}

func TestNewFromJsonGeneratorErrorsOnBadBodyFile(t *testing.T) {
	base := Request{Url: mustParseURL(t, "http://base.test"), Method: http.MethodGet}

	path := writeTempFile(t, `[
		{"method":"POST","path":"/missing","body_file":"no_such_body_file.json"}
	]`)
	_, err := NewFromJsonGenerator(base, path)
	if err == nil {
		t.Fatal("expected error for unreadable body_file, got nil")
	}
	if !strings.Contains(err.Error(), "could not read body_file") {
		t.Errorf("error = %q, want body_file read failure", err)
	}

	path = writeTempFile(t, `[
		{"method":"POST","path":"/both","body":"inline","body_file":"examples/payload.json"}
	]`)
	_, err = NewFromJsonGenerator(base, path)
	if err == nil {
		t.Fatal("expected error for body and body_file together, got nil")
	}
	if !strings.Contains(err.Error(), "both body and body_file") {
		t.Errorf("error = %q, want body/body_file conflict", err)
	}
}

func TestNewFromJsonGeneratorResolvesContentType(t *testing.T) {
	base := Request{Url: mustParseURL(t, "http://base.test"), Method: http.MethodGet}

	path := writeTempFile(t, `[
		{"method":"POST","path":"/explicit","body":"b","content_type":"application/x-ndjson"},
		{"method":"POST","path":"/header-wins","body":"b","content_type":"application/x-ndjson","headers":{"Content-Type":"text/plain"}},
		{"method":"POST","path":"/no-content-type","body":"b"},
		{"method":"POST","path":"/from-extension","body_file":"examples/payload.json"},
		{"method":"POST","path":"/extension-overridden","body_file":"examples/payload.json","content_type":"text/csv"}
	]`)
	gen, err := NewFromJsonGenerator(base, path)
	if err != nil {
		t.Fatalf("could not create generator: %s", err)
	}
	reqs := gen.sourceRequests

	want := []string{
		"application/x-ndjson", // content_type field
		"text/plain",           // explicit header beats content_type
		"",                     // nothing set
		"application/json",     // .json extension of body_file
		"text/csv",             // content_type beats extension
	}
	for i, wantValue := range want {
		if got := reqs[i].Headers["Content-Type"]; got != wantValue {
			t.Errorf("request %d Content-Type = %q, want %q", i, got, wantValue)
		}
	}
}

func TestFromJsonGeneratorLoadsBodyFileExamplePayload(t *testing.T) {
	base := Request{Url: mustParseURL(t, "http://base.test"), Method: http.MethodGet}
	gen, err := NewFromJsonGenerator(base, "examples/payload_body_file.json")
	if err != nil {
		t.Fatalf("could not load body_file example payload: %s", err)
	}
	for i, req := range gen.sourceRequests {
		if req.Body == "" {
			t.Errorf("request %d body is empty, want content of %q", i, req.BodyFile)
		}
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
