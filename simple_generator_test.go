package main

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestSimpleRequestGeneratorRepeatsBaseRequest(t *testing.T) {
	base := Request{
		Url:    mustParseURL(t, "http://base.test/root"),
		Method: http.MethodPost,
		Body:   "payload",
	}
	gen := NewSimpleRequestGenerator(base)

	ctx, cancel := context.WithCancel(context.Background())
	requests := make(chan *Request)
	go gen.GenerateRequests(ctx, requests)

	for range 5 {
		select {
		case req := <-requests:
			if req.Url == nil || req.Url.String() != "http://base.test/root" {
				t.Errorf("generated url = %v, want http://base.test/root", req.Url)
			}
			if req.Method != http.MethodPost {
				t.Errorf("generated method = %q, want %q", req.Method, http.MethodPost)
			}
			if req.Body != "payload" {
				t.Errorf("generated body = %q, want %q", req.Body, "payload")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("generator stalled")
		}
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
