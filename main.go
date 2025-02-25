package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"
)

var concurrency int
var timeout time.Duration
var duration time.Duration
var initialUrl string
var fromJson string
var maxproc int

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

	flag.StringVar(&fromJson, "from_json", "", "take requests from json")
	flag.IntVar(&maxproc, "maxproc", 0, "GOMAXPROC runtime setting")

	flag.Parse()
}

type Request struct {
	Url         string        `json:"url"`
	Path        string        `json:"path"`
	Host        string        `json:"host"`
	Method      string        `json:"method"`
	Body        string        `json:"body"`
	MaxDuration time.Duration `json:"-"`
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
		// DialContext: func(ctx context.Context, network string, addr string) (net.Conn, error) {
		// 	conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		// 	if err != nil {
		// 		return conn, err
		// 	}
		// 	switch tcp := conn.(type) {
		// 	case *net.TCPConn:
		// 		err = tcp.SetNoDelay(true)
		// 		if err != nil {
		// 			return conn, err
		// 		}
		// 	}
		// 	return conn, err
		// },
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
	statusMap := make(map[int]int)

	ticker := time.NewTicker(500 * time.Millisecond)

	for result := range results {
		totalResponses++
		totalDuration += result.Latency
		if totalResponses == 1 {
			testStart = time.Now().Add(-result.Latency)
			fmt.Printf("test started: %s\n", testStart)
		}
		statusMap[result.StatusCode]++

		select {
		case <-ticker.C:
			fmt.Printf("responses total: %d; avg rps: %f; avg duration %s; \n",
				totalResponses,
				float64(totalResponses)/time.Since(testStart).Seconds(),
				totalDuration/time.Duration(totalResponses),
			)
		default:
		}
	}

	fmt.Printf("test ended: %s\n", time.Now())

	fmt.Printf("status stat:")
	for status, count := range statusMap {
		fmt.Printf("> status: %d count %d\n", status, count)
	}
	fmt.Printf("responses total: %d\n", totalResponses)
	fmt.Printf("avg rps: %f\n", float64(totalResponses)/time.Since(testStart).Seconds())
	fmt.Printf("avg duration: %s\n", totalDuration/time.Duration(totalResponses))
}

type RequestGenerator interface {
	GenerateRequests(ctx context.Context, requests chan<- *http.Request)
}

func main() {
	fmt.Printf("concurrency: %d\n", concurrency)
	fmt.Printf("duration: %s\n", duration)
	fmt.Printf("timeout: %s\n", timeout)
	fmt.Printf("url: %s\n", initialUrl)
	var generator RequestGenerator
	var err error
	if fromJson != "" {
		generator, err = NewFromJsonGenerator(Request{
			Url:         initialUrl,
			Method:      http.MethodGet,
			MaxDuration: timeout,
		}, fromJson)
		if err != nil {
			log.Fatalf("could not create requests from json: %s\n", err)
		}
	} else {
		generator = NewSimpleRequestGenerator(Request{
			Url:         initialUrl,
			Method:      http.MethodGet,
			MaxDuration: timeout,
		})
	}
	if maxproc > 0 {
		fmt.Printf("max proc: %d\n", maxproc)
		runtime.GOMAXPROCS(maxproc)
	}

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
	generator.GenerateRequests(ctx, requests)
	fmt.Println("wrapping up")
	wgDone.Wait()
	close(results)
	wg.Wait()
}
