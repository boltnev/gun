package main

import (
	"context"
)

type SimpleRequestGenerator struct {
	baseRequest Request
	// logger etc
}

// PreparedRequests is the single request template the generator loops over.
func (gen *SimpleRequestGenerator) PreparedRequests() int {
	return 1
}

func NewSimpleRequestGenerator(baseRequest Request) *SimpleRequestGenerator {
	return &SimpleRequestGenerator{
		baseRequest: baseRequest,
	}
}

func (gen *SimpleRequestGenerator) GenerateRequests(ctx context.Context, requests chan<- *Request) {
out:
	for {
		select {
		case requests <- &gen.baseRequest:
		case <-ctx.Done():
			break out
		}
	}
	close(requests)
}
