package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const DefaultUserAgent = "Gun LoadTesting Tool/0.1"

// applyHeaders layers headers onto the request: -H flags first, then
// per-request headers from the json payload (they win), with the default
// user agent filling whatever is left.
func applyHeaders(hr *http.Request, req *Request) {
	for _, header := range cliHeaders {
		setHeader(hr, header[0], header[1])
	}
	for name, value := range req.Headers {
		setHeader(hr, name, value)
	}
	if hr.Header.Get("User-Agent") == "" {
		hr.Header.Set("User-Agent", DefaultUserAgent)
	}
}

// setHeader routes the Host header to the dedicated request field: net/http
// ignores "Host" in the header map for client requests.
func setHeader(hr *http.Request, name, value string) {
	if strings.EqualFold(name, "Host") {
		hr.Host = value
		return
	}
	hr.Header.Set(name, value)
}

// applyCookies appends the per-request cookie map to the Cookie header, on
// top of anything -H flags or headers already set; names are sorted so the
// header value is stable.
func applyCookies(hr *http.Request, req *Request) {
	names := make([]string, 0, len(req.Cookies))
	for name := range req.Cookies {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		hr.AddCookie(&http.Cookie{Name: name, Value: req.Cookies[name]})
	}
}

type HttpRunner struct {
	threadNum int
	client    *http.Client
}

func NewHttpRunner(threadNum int, wgReady *sync.WaitGroup) *HttpRunner {
	defer wgReady.Done()
	tr := &http.Transport{
		MaxIdleConnsPerHost: 1024,
		TLSHandshakeTimeout: 0 * time.Second,
		MaxConnsPerHost:     concurrency,
	}
	client := http.Client{Transport: tr}
	fmt.Printf("thread %d is ready\n", threadNum)
	return &HttpRunner{
		client:    &client,
		threadNum: threadNum,
	}
}

func (h *HttpRunner) Run(ctx context.Context, wgDone *sync.WaitGroup, requests <-chan *Request, results chan<- Result) {
	defer wgDone.Done()

out:
	for req := range requests {
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		if req.Url == nil {
			fatal("wrong request without url: %s", req.UrlRaw)
		}
		httpReq, err := http.NewRequestWithContext(
			reqCtx,
			req.Method,
			req.Url.String(),
			bytes.NewBuffer([]byte(req.Body)),
		)
		if err != nil {
			fatal("could not create request: %s", err)
		}
		applyHeaders(httpReq, req)
		applyCookies(httpReq, req)
		start := time.Now()
		response, err := h.client.Do(httpReq)
		// client.Do returns once the response headers are received, which is
		// the first byte of the response on the wire.
		firstByteLatency := time.Since(start)
		statusCode := 0
		var sizeBytes int64
		if response != nil {
			statusCode = response.StatusCode
			if response.Body != nil {
				sizeBytes, _ = io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
		}
		latency := time.Since(start)
		cancel()
		results <- Result{
			Latency:          latency,
			FirstByteLatency: firstByteLatency,
			SizeBytes:        sizeBytes,
			StatusCode:       statusCode,
			err:              err,
		}
		select {
		case <-ctx.Done():
			break out
		default:
		}
	}
}
