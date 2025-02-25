package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
)

type SimpleRequestGenerator struct {
	baseRequest Request
	// logger etc
}

func NewSimpleRequestGenerator(baseRequest Request) *SimpleRequestGenerator {
	return &SimpleRequestGenerator{
		baseRequest: baseRequest,
	}
}

func (gen *SimpleRequestGenerator) GenerateRequests(ctx context.Context, requests chan<- *http.Request) {
	baseUrl := gen.baseRequest.Url
	parsedUrl, err := url.Parse(baseUrl)
	if err != nil {
		log.Fatalf("url is not valid: %s", parsedUrl)
	}
out:
	for {
		reqCtx, cancel := context.WithTimeout(ctx, gen.baseRequest.MaxDuration)

		defer cancel()

		req, err := http.NewRequestWithContext(
			reqCtx,
			gen.baseRequest.Method,
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
