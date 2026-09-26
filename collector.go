package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

type SimpleCollector struct {
	// logger
}

func NewSimpleCollector() *SimpleCollector {
	return &SimpleCollector{}
}

func (s *SimpleCollector) Collect(ctx context.Context, wg *sync.WaitGroup, results <-chan Result) {
	defer wg.Done()
	var totalDuration time.Duration = 0
	var totalResponses int = 0
	var testStart time.Time
	statusMap := make(map[int]int)
	errsCount := 0

	// percentile stats over successful responses only; TTFB and response
	// size are recorded by the HTTP runner, so they stay empty in qdrant mode
	var latencies Samples
	var firstByteLatencies Samples
	var sizes Samples

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
		} else {
			latencies.Observe(int64(result.Latency))
			if loadType == LoadTypeHTTP {
				firstByteLatencies.Observe(int64(result.FirstByteLatency))
				sizes.Observe(result.SizeBytes)
			}
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
			fmt.Printf("responses total: %d; avg rps: %f; avg points count: %f; avg max score: %f\n",
				totalResponses,
				float64(totalResponses)/time.Since(testStart).Seconds(),
				float64(pointsFound)/float64(totalResponses),
				float64(maxScore)/float64(totalResponses),
			)
		}
	default:
	}
	fmt.Printf("avg rps: %f\n", float64(totalResponses)/time.Since(testStart).Seconds())
	if latencies.Count() > 0 {
		fmt.Printf("latency stats (%d responses): %s\n", latencies.Count(), DurationStatsLine(&latencies))
	} else {
		fmt.Printf("latency stats: no successful responses\n")
	}
	if firstByteLatencies.Count() > 0 {
		fmt.Printf("time to first byte stats (%d responses): %s\n", firstByteLatencies.Count(), DurationStatsLine(&firstByteLatencies))
	}
	if sizes.Count() > 0 {
		fmt.Printf("response size stats in bytes (%d responses): %s\n", sizes.Count(), SizeStatsLine(&sizes))
	}
}
