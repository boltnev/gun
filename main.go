package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"sync"
	"time"
)

var concurrency int
var timeout time.Duration
var duration time.Duration
var initialUrl string

const ResultBufferSize = 1024 * 1024 * 20

func init() {
	flag.IntVar(&concurrency, "c", 1, "concurrency threads")
	flag.IntVar(&concurrency, "concurrency", 1, "concurrency threads")

	flag.DurationVar(&timeout, "t", time.Second*10, "request timeout")
	flag.DurationVar(&timeout, "timeout", time.Second*10, "request timeout")

	flag.DurationVar(&duration, "d", time.Minute, "test duration")
	flag.DurationVar(&duration, "duration", time.Minute, "test duration")

	flag.StringVar(&initialUrl, "u", "http://localhost", "url to reqest")
	flag.StringVar(&initialUrl, "url", "http://localhost", "url to request")

	flag.Parse()
}

type Request struct {
	Url         string
	Host        string
	Method      string
	Body        []byte
	MaxDuration time.Duration
	Context     context.Context
}

type Result struct {
	Latency    time.Duration
	StatusCode int
	err        error
}

func Run(thread int, wgReady *sync.WaitGroup, wgDone *sync.WaitGroup, ctx context.Context, requests <-chan *http.Request, results chan<- Result) {
	tr := &http.Transport{
		MaxIdleConnsPerHost: 1024,
		TLSHandshakeTimeout: 0 * time.Second,
	}
	client := http.Client{Transport: tr}
	fmt.Printf("thread %d is ready\n", thread)
	wgReady.Done()
	defer wgDone.Done()

out:
	for req := range requests {
		start := time.Now()
		response, err := client.Do(req)
		statusCode := 0
		if response != nil {
			statusCode = response.StatusCode
			if response.Body != nil {
				io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
		}
		results <- Result{
			Latency:    time.Since(start),
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

func Collect(wg *sync.WaitGroup, results <-chan Result) {
	defer wg.Done()
	var totalDuration time.Duration = 0
	var totalResponses int = 0

	var testStart time.Time

	for result := range results {
		totalResponses++
		totalDuration += result.Latency
		if totalResponses == 1 {
			testStart = time.Now().Add(-result.Latency)
			fmt.Printf("test started: %s\n", testStart)
		}
	}
	fmt.Printf("test ended: %s\n", time.Now())
	fmt.Printf("responses total: %d\n", totalResponses)
	fmt.Printf("avg rps: %f\n", float64(totalResponses)/time.Since(testStart).Seconds())
	fmt.Printf("avg duration: %s\n", totalDuration/time.Duration(totalResponses))
}

func GenerateRequests(ctx context.Context, requests chan<- *http.Request) {
	parsedUrl, err := url.Parse(initialUrl)
	if err != nil {
		log.Fatalf("url is not valid: %s", parsedUrl)
	}
out:
	for {
		reqCtx, cancel := context.WithTimeout(ctx, timeout)

		defer cancel()

		req, err := http.NewRequestWithContext(
			reqCtx,
			http.MethodGet,
			parsedUrl.String(),
			nil,
		)
		if err != nil {
			select {
			case <-ctx.Done():
				break out
			default:
				log.Fatalf("could not create request: %s", err)
			}
			break
		}
		select {
		case requests <- req:
		case <-ctx.Done():
			break out
		}
	}
	close(requests)
}

func main() {
	fmt.Printf("concurrency: %d\n", concurrency)
	fmt.Printf("duration: %s\n", duration)
	fmt.Printf("timeout: %s\n", timeout)
	fmt.Printf("url: %s\n", initialUrl)
	runtime.GOMAXPROCS(4)

	requests := make(chan *http.Request)
	results := make(chan Result, ResultBufferSize)
	ctx, cancel := context.WithTimeout(context.Background(), duration+timeout)
	defer cancel()

	wg := sync.WaitGroup{}
	wgDone := sync.WaitGroup{}

	// runners
	wg.Add(concurrency)
	wgDone.Add(concurrency)

	for threadNum := range concurrency {
		go Run(threadNum, &wg, &wgDone, ctx, requests, results)
	}
	wg.Wait()

	wg.Add(1)
	// Collect and display
	go Collect(&wg, results)
	// generator
	ctx, cancel = context.WithTimeout(ctx, duration)
	defer cancel()
	GenerateRequests(ctx, requests)
	fmt.Println("wrapping up")
	wgDone.Wait()
	close(results)
	wg.Wait()
}
