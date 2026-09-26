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
	"golang.org/x/sys/unix"
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
// place. Progress output is terminal-only: without a terminal the mid-run
// section is skipped entirely so logs and pipes stay clean.
func (s *SimpleCollector) redraw(lines []string) {
	if !s.interactive {
		return
	}
	// a line wider than the terminal wraps onto a second physical row, which
	// breaks the move-up-N-lines arithmetic — clip every line to the width
	width := terminalWidth()
	var b strings.Builder
	if s.progressLines > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", s.progressLines) // cursor up to the first drawn line
	}
	for _, line := range lines {
		fmt.Fprintf(&b, "\x1b[2K%s\n", truncateLine(line, width)) // clear line, write, step down
	}
	// erase leftovers when the new block has fewer lines than the last one
	b.WriteString("\x1b[J")
	fmt.Print(b.String())
	s.progressLines = len(lines)
}

// terminalWidth returns the stdout width in terminal cells, or 0 when it
// cannot be determined.
func terminalWidth() int {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 {
		return 0
	}
	return int(ws.Col)
}

// truncateLine clips a report line to the terminal width.
func truncateLine(line string, width int) string {
	if width <= 0 {
		return line
	}
	runes := []rune(line)
	if len(runes) <= width {
		return line
	}
	return string(runes[:width])
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

	// transfer accounting: request wire bytes and response header bytes are
	// estimated by the HTTP runner; response body bytes come via sizes
	sentBytesTotal := int64(0)
	recvHeaderBytesTotal := int64(0)

	// TODO: refactor qdrant stuff
	pointsFound := 0
	maxScore := float64(0)

	// qdrantAvg renders the qdrant-specific summary line shared by the
	// progress and finish sections.
	qdrantAvg := func() string {
		return fmt.Sprintf("%.1f   max score avg %.3f",
			float64(pointsFound)/float64(totalResponses),
			maxScore/float64(totalResponses),
		)
	}

	ticker := time.NewTicker(500 * time.Millisecond)
out:
	for result := range results {
		totalResponses++
		totalDuration += result.Latency
		if totalResponses == 1 {
			testStart = time.Now().Add(-result.Latency)
		}
		statusMap[result.StatusCode]++
		sentBytesTotal += result.SentBytes
		recvHeaderBytesTotal += result.RecvHeaderBytes
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
			elapsed := time.Since(testStart)
			lines := []string{
				kv("running", fmt.Sprintf("%s/%s  %s",
					formatNs(float64(elapsed)), formatNs(float64(duration)), progressBar(elapsed, duration))),
				kv("responses", fmt.Sprintf("%s   rps %.1f   errors %s",
					formatCount(totalResponses),
					float64(totalResponses)/elapsed.Seconds(), formatCount(errsCount))),
			}
			if loadType == LoadTypeQdrant {
				lines = append(lines, kv("points avg", qdrantAvg()))
			} else {
				lines = append(lines, kv("statuses", statusCounts(statusMap)))
				lines = append(lines, kv("transfer", fmt.Sprintf("sent %s/s   recv %s/s",
					humanBytes(float64(sentBytesTotal)/elapsed.Seconds()),
					humanBytes(float64(sizes.Total()+recvHeaderBytesTotal)/elapsed.Seconds()))))
			}
			lines = append(lines, "")
			lines = append(lines, statsTableLines(&latencies, &firstByteLatencies, &sizes)...)
			s.redraw(lines)
		case <-ctx.Done():
			break out
		default:
		}
	}

	s.clearProgress()
	elapsed := time.Since(testStart)
	rps := float64(totalResponses) / elapsed.Seconds()
	lines := []string{
		separator,
		kv("finished", fmt.Sprintf("%s   elapsed %s",
			time.Now().Format("2006-01-02 15:04:05"), formatNs(float64(elapsed)))),
		kv("responses", fmt.Sprintf("%s   rps %.1f   errors %s",
			formatCount(totalResponses), rps, formatCount(errsCount))),
	}
	switch loadType {
	case LoadTypeHTTP:
		lines = append(lines, kv("statuses", statusCounts(statusMap)))
	case LoadTypeQdrant:
		if totalResponses > 0 {
			lines = append(lines, kv("points avg", qdrantAvg()))
		}
	default:
	}
	if latencies.Count() == 0 {
		lines = append(lines, "  no successful responses")
	} else {
		lines = append(lines, "")
		lines = append(lines, statsTableLines(&latencies, &firstByteLatencies, &sizes)...)
	}
	if loadType == LoadTypeHTTP && totalResponses > 0 {
		recvTotal := sizes.Total() + recvHeaderBytesTotal
		lines = append(lines, "",
			kv("sent", fmt.Sprintf("%s total   %s/s",
				humanBytes(float64(sentBytesTotal)),
				humanBytes(float64(sentBytesTotal)/elapsed.Seconds()))),
			kv("recv", fmt.Sprintf("%s total   %s/s",
				humanBytes(float64(recvTotal)),
				humanBytes(float64(recvTotal)/elapsed.Seconds()))),
		)
	}
	for _, line := range lines {
		fmt.Println(line)
	}
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
