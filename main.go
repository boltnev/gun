package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/qdrant/go-client/qdrant"
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
)

var concurrency int
var timeout time.Duration
var duration time.Duration
var initialUrlRaw string
var initialUrl *url.URL
var fromJson string
var maxproc int
var loadType string

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

func Collect(ctx context.Context, wg *sync.WaitGroup, results <-chan Result) {
	defer wg.Done()
	var totalDuration time.Duration = 0
	var totalResponses int = 0
	var testStart time.Time
	statusMap := make(map[int]int)
	errsCount := 0

	// TODO: refactor qdrant stuff
	pointsFound := 0
	maxScore := float64(0)

	ticker := time.NewTicker(500 * time.Millisecond)
out:
	for result := range results {
		totalResponses++
		totalDuration += result.Latency
		if totalResponses == 1 {
			testStart = time.Now().Add(-result.Latency)
			fmt.Printf("test started: %s\n", testStart)
		}
		statusMap[result.StatusCode]++
		if result.err != nil {
			errsCount++
		}
		switch data := result.AnyData.(type) {
		case []*qdrant.ScoredPoint:
			pointsFound += len(data)
			score := float32(0)
			for _, point := range data {
				if point.Score > score {
					score = point.Score
				}
			}
			maxScore += float64(score)
		default:
		}
		select {
		case <-ticker.C:
			switch loadType {
			case LoadTypeQdrant:
				fmt.Printf("responses total: %d; avg rps: %f; avg duration %s; avg points count: %f; avg max score: %f\n",
					totalResponses,
					float64(totalResponses)/time.Since(testStart).Seconds(),
					totalDuration/time.Duration(totalResponses),
					float64(pointsFound)/float64(totalResponses),
					float64(maxScore)/float64(totalResponses),
				)
			default:
				fmt.Printf("responses total: %d; avg rps: %f; avg duration %s; \n",
					totalResponses,
					float64(totalResponses)/time.Since(testStart).Seconds(),
					totalDuration/time.Duration(totalResponses),
				)
			}
		case <-ctx.Done():
			break out
		default:
		}
	}

	fmt.Printf("test ended: %s\n", time.Now())
	fmt.Printf("responses total: %d\n", totalResponses)
	switch loadType {
	case LoadTypeHTTP:
		fmt.Printf("http status stats:\n")
		for status, count := range statusMap {
			fmt.Printf(">>> status: %d count %d\n", status, count)
		}
	case LoadTypeQdrant:
		fmt.Printf("qdrant status request errors %d:\n", errsCount)
		if totalResponses > 0 {
			fmt.Printf("responses total: %d; avg rps: %f; avg duration %s; avg points count: %f; avg max score: %f\n",
				totalResponses,
				float64(totalResponses)/time.Since(testStart).Seconds(),
				totalDuration/time.Duration(totalResponses),
				float64(pointsFound)/float64(totalResponses),
				float64(maxScore)/float64(totalResponses),
			)
		}
	default:
	}
	fmt.Printf("avg rps: %f\n", float64(totalResponses)/time.Since(testStart).Seconds())
	if totalResponses > 0 {
		fmt.Printf("avg duration: %s\n", totalDuration/time.Duration(totalResponses))
	}
}

type Runner interface {
	Run(ctx context.Context, wgDone *sync.WaitGroup, requests <-chan *Request, results chan<- Result)
}

type RequestGenerator interface {
	GenerateRequests(ctx context.Context, requests chan<- *Request)
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
	var err error
	if fromJson != "" {
		switch loadType {
		case LoadTypeHTTP:
			generator, err = NewFromJsonGenerator(Request{
				Url:         initialUrl,
				Method:      http.MethodGet,
				MaxDuration: timeout,
			}, fromJson)
			if err != nil {
				log.Fatalf("could not create requests from json: %s\n", err)
			}
		case LoadTypeQdrant:
			generator, err = NewQdrantFromJsonGenerator(fromJson)
			if err != nil {
				log.Fatalf("could not create requests from json: %s\n", err)
			}
		default:
			log.Fatalf("error on load generator select: unknown load type: %s\n", loadType)
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

	wgDone.Add(concurrency)
	for _, runner := range runners {
		go runner.Run(ctx, &wgDone, requests, results)
	}

	wg.Add(1)
	// Collect and display
	ctxCollection, cancelCollection := context.WithTimeout(ctx, duration)
	go Collect(ctxCollection, &wg, results)
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
