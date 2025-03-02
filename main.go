package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"

	"golang.org/x/time/rate"
)

var concurrency int
var timeout time.Duration
var duration time.Duration
var initialUrlRaw string
var initialUrl *url.URL
var fromJson string
var maxproc int
var loadType string
var rateLimit float64
var burstLimit int

var cpuprofile string
var memprofile string

const ResultBufferSize = 1024 * 1024 * 20

const (
	LoadTypeHTTP   = "http"
	LoadTypeQdrant = "qdrant"
)

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

	flag.StringVar(&loadType, "load_type", "http", "load type. allowed: http, qdrant")

	flag.Float64Var(&rateLimit, "rate", 0, "rate limit")
	flag.IntVar(&burstLimit, "burst", 0, "burst limit")

	flag.Parse()
	var err error
	initialUrl, err = url.Parse(initialUrlRaw)
	if err != nil {
		log.Fatalf("bad url: %s", initialUrl)
	}
	allowedloadTypes := []string{"http", "qdrant"}
	notAllowed := true
	for _, allowedType := range allowedloadTypes {
		if loadType == allowedType {
			notAllowed = false
		}
	}
	if notAllowed {
		log.Fatalf("not allowed load type: %s", loadType)
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
	AnyData     any           `json:"-"`
}

type Requests = []Request

type Result struct {
	Latency    time.Duration
	StatusCode int
	err        error

	AnyData any
}

type Runner interface {
	Run(ctx context.Context, wgDone *sync.WaitGroup, requests <-chan *Request, results chan<- Result)
}

type RequestGenerator interface {
	GenerateRequests(ctx context.Context, requests chan<- *Request)
}

type Collector interface {
	Collect(ctx context.Context, wg *sync.WaitGroup, results <-chan Result)
}

type RateLimiter interface {
	RateLimit(in <-chan *Request, out chan<- *Request)
}

func main() {
	fmt.Printf("concurrency: %d\n", concurrency)
	fmt.Printf("duration: %s\n", duration)
	fmt.Printf("timeout: %s\n", timeout)
	fmt.Printf("url: %s\n", initialUrlRaw)
	fmt.Printf("load_type: %s\n", loadType)
	fmt.Print("============")
	var runners []Runner
	var generator RequestGenerator
	var collector Collector

	var err error
	if fromJson != "" {
		switch loadType {
		case LoadTypeHTTP:
			generator, err = NewFromJsonGenerator(Request{
				Url:         initialUrl,
				Method:      http.MethodGet,
				MaxDuration: timeout,
			}, fromJson)
		case LoadTypeQdrant:
			generator, err = NewQdrantFromJsonGenerator(fromJson)
		default:
			log.Fatalf("error on load generator select: unknown load type: %s\n", loadType)
		}
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

	for threadNum := range concurrency {
		switch loadType {
		case LoadTypeHTTP:
			runners = append(runners, NewHttpRunner(threadNum, &wg))
		case LoadTypeQdrant:
			runners = append(runners, NewQdrantRunner(threadNum, &wg, initialUrlRaw))
		default:
			log.Fatalf("error on load runner select: unknown load type: %s\n", loadType)
		}
	}
	wg.Wait()

	var reqChannel chan *Request = requests

	if rateLimit > 0 {
		requestsLimited := make(chan *Request)
		if burstLimit == 0 {
			burstLimit = int(rateLimit)
		}

		rateLimiter := NewTokenBucketRateLimiter(rate.Limit(rateLimit), burstLimit)
		reqChannel = requestsLimited
		go rateLimiter.RateLimit(requests, requestsLimited)
	}

	wgDone.Add(concurrency)
	for _, runner := range runners {
		go runner.Run(ctx, &wgDone, reqChannel, results)
	}

	wg.Add(1)
	// Collect and display
	ctxCollection, cancelCollection := context.WithTimeout(ctx, duration)
	collector = NewSimpleCollector()

	go collector.Collect(ctxCollection, &wg, results)
	// generator
	defer func() {
		if ctx.Err() == nil {
			cancel()
		}
	}()
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	go generator.GenerateRequests(ctx, requests)

	select {
	case <-done:
		cancel()
		cancelCollection()
	case <-ctx.Done():
	}

	fmt.Println("wrapping up")
	wgDone.Wait()
	close(results)
	wg.Wait()
}
