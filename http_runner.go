package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

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
			log.Fatalf("wrong request without url: %s", req.UrlRaw)
		}
		httpReq, err := http.NewRequestWithContext(
			reqCtx,
			req.Method,
			req.Url.String(),
			bytes.NewBuffer([]byte(req.Body)),
		)
		if err != nil {
			log.Fatalf("could not create request: %s", err)
		}
		start := time.Now()
		response, err := h.client.Do(httpReq)
		statusCode := 0
		if response != nil {
			statusCode = response.StatusCode
			if response.Body != nil {
				io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
		}
		latency := time.Since(start)
		cancel()
		results <- Result{
			Latency:    latency,
			StatusCode: statusCode,
			err:        err,
		}
		select {
		case <-ctx.Done():
			break out
		default:
		}
	}
}
