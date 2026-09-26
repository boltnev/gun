package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

type SimpleCollector struct {
	// interactive redraws the mid-run progress in place instead of appending
	// new lines; it is true when stdout is a terminal
	interactive bool
	// progressLines is the number of lines drawn by the last redraw
	progressLines int
}

func NewSimpleCollector() *SimpleCollector {
	return &SimpleCollector{interactive: stdoutIsTerminal()}
}

// stdoutIsTerminal reports whether stdout is a character device: piped or
// redirected output must not receive ANSI cursor codes.
func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// redraw erases the previously drawn lines and prints the new block in their
// place; without a terminal it appends the lines the plain way.
func (s *SimpleCollector) redraw(lines []string) {
	if !s.interactive {
		for _, line := range lines {
			fmt.Println(line)
		}
		return
	}
	var b strings.Builder
	if s.progressLines > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", s.progressLines) // cursor up to the first drawn line
	}
	for _, line := range lines {
		fmt.Fprintf(&b, "\x1b[2K%s\n", line) // clear line, write, step down
	}
	// erase leftovers when the new block has fewer lines than the last one
	b.WriteString("\x1b[J")
	fmt.Print(b.String())
	s.progressLines = len(lines)
}

// clearProgress erases the progress block so the final report starts on a
// clean screen area.
func (s *SimpleCollector) clearProgress() {
	if !s.interactive || s.progressLines == 0 {
		return
	}
	fmt.Printf("\x1b[%dA\x1b[J", s.progressLines)
	s.progressLines = 0
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
			summary := fmt.Sprintf("responses total: %d; avg rps: %f; avg duration %s",
				totalResponses,
				float64(totalResponses)/time.Since(testStart).Seconds(),
				totalDuration/time.Duration(totalResponses),
			)
			if loadType == LoadTypeQdrant {
				summary += fmt.Sprintf("; avg points count: %f; avg max score: %f; errors: %d",
					float64(pointsFound)/float64(totalResponses),
					float64(maxScore)/float64(totalResponses),
					errsCount,
				)
			}
			lines := []string{summary}
			if loadType == LoadTypeHTTP {
				lines = append(lines, "statuses: "+statusCounts(statusMap))
			}
			lines = append(lines, statsBlockLines(&latencies, &firstByteLatencies, &sizes)...)
			s.redraw(lines)
		case <-ctx.Done():
			break out
		default:
		}
	}

	s.clearProgress()
	fmt.Printf("test ended: %s\n", time.Now())
	fmt.Printf("responses total: %d\n", totalResponses)
	switch loadType {
	case LoadTypeHTTP:
		fmt.Printf("http status stats: %s\n", statusCounts(statusMap))
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
	if latencies.Count() == 0 {
		fmt.Printf("latency stats: no successful responses\n")
	} else {
		for _, line := range statsBlockLines(&latencies, &firstByteLatencies, &sizes) {
			fmt.Println(line)
		}
	}
}

// statsBlockLines returns one line per stats block that has samples; shared
// by the mid-run progress redraw and the final report.
func statsBlockLines(latencies, firstByteLatencies, sizes *Samples) []string {
	var lines []string
	if latencies.Count() > 0 {
		lines = append(lines, fmt.Sprintf("latency stats (%d responses): %s", latencies.Count(), DurationStatsLine(latencies)))
	}
	if firstByteLatencies.Count() > 0 {
		lines = append(lines, fmt.Sprintf("time to first byte stats (%d responses): %s", firstByteLatencies.Count(), DurationStatsLine(firstByteLatencies)))
	}
	if sizes.Count() > 0 {
		lines = append(lines, fmt.Sprintf("response size stats in bytes (%d responses): %s", sizes.Count(), SizeStatsLine(sizes)))
	}
	return lines
}

// statusCounts renders the status counter as deterministic "code=count"
// pairs sorted by code; status 0 marks transport-level errors.
func statusCounts(statusMap map[int]int) string {
	codes := make([]int, 0, len(statusMap))
	for code := range statusMap {
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		return "(none)"
	}
	sort.Ints(codes)
	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		parts = append(parts, fmt.Sprintf("%d=%d", code, statusMap[code]))
	}
	return strings.Join(parts, ", ")
}
