package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"
)

var concurrency int
var timeout time.Duration
var duration time.Duration
var initialUrlRaw string
var initialUrl *url.URL
var fromJson string
var maxproc int

var cpuprofile string
var memprofile string

const ResultBufferSize = 1024 * 1024 * 20

func init() {
	flag.IntVar(&concurrency, "c", 1, "concurrency threads")
	flag.IntVar(&concurrency, "concurrency", 1, "concurrency threads")

	flag.DurationVar(&timeout, "t", time.Second*10, "request timeout")
	flag.DurationVar(&timeout, "timeout", time.Second*10, "request timeout")

	flag.DurationVar(&duration, "d", time.Minute, "test duration")
	flag.DurationVar(&duration, "duration", time.Minute, "test duration")

	flag.StringVar(&initialUrlRaw, "u", "http://localhost", "url to reqest")
	flag.StringVar(&initialUrlRaw, "url", "http://localhost", "url to request")

	flag.StringVar(&fromJson, "from_json", "", "take requests from json")
	flag.IntVar(&maxproc, "maxproc", 0, "GOMAXPROC runtime setting")

	flag.StringVar(&cpuprofile, "cpuprofile", "", "cpuprofile filepath")
	flag.StringVar(&memprofile, "memprofile", "", "memprofile filepath; writing memprofile in the middle of the test")

	flag.Parse()
	var err error
	initialUrl, err = url.Parse(initialUrlRaw)
	if err != nil {
		log.Fatal("bad url: %s", initialUrl)
	}
}

type Request struct {
	UrlRaw string `json:"url,omitempty"`
	Path   string `json:"path,omitempty"`
	Host   string `json:"host,omitempty"`
	Method string `json:"method,omitempty"`
	Body   string `json:"body,omitempty"`

	MaxDuration time.Duration `json:"-"`
	Url         *url.URL      `json:"-"`
}

type Result struct {
	Latency    time.Duration
	StatusCode int
	err        error
}

func Run(thread int, wgReady *sync.WaitGroup, wgDone *sync.WaitGroup, ctx context.Context, requests <-chan *Request, results chan<- Result) {
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

		response, err := client.Do(httpReq)
		statusCode := 0
		if response != nil {
			statusCode = response.StatusCode
			if response.Body != nil {
				io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
		}
		cancel()
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
	fmt.Printf("http status stats:\n")
	for status, count := range statusMap {
		fmt.Printf(">>> status: %d count %d\n", status, count)
	}
	fmt.Printf("responses total: %d\n", totalResponses)
	fmt.Printf("avg rps: %f\n", float64(totalResponses)/time.Since(testStart).Seconds())
	fmt.Printf("avg duration: %s\n", totalDuration/time.Duration(totalResponses))
}

type RequestGenerator interface {
	GenerateRequests(ctx context.Context, requests chan<- *Request)
}

func main() {
	fmt.Printf("concurrency: %d\n", concurrency)
	fmt.Printf("duration: %s\n", duration)
	fmt.Printf("timeout: %s\n", timeout)
	fmt.Printf("url: %s\n", initialUrlRaw)
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
	if cpuprofile != "" {
		f, err := os.Create(cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	if memprofile != "" {
		go func() {
			time.Sleep(duration / 2)
			fmt.Println("writing memprofile")
			f, err := os.Create(memprofile)
			if err != nil {
				log.Fatal(err)
			}
			pprof.WriteHeapProfile(f)

		}()
	}
	if maxproc > 0 {
		fmt.Printf("max proc: %d\n", maxproc)
		runtime.GOMAXPROCS(maxproc)
	}

	requests := make(chan *Request)
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
