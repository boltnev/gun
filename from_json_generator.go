package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
)

type FromJsonGenerator struct {
	baseRequest    Request
	sourceRequests Requests
	// logger etc
}

func NewFromJsonGenerator(baseRequest Request, sourceFilePath string) (*FromJsonGenerator, error) {
	requestsFromJson := Requests{}
	sourceBytes, err := os.ReadFile(sourceFilePath)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(sourceBytes, &requestsFromJson)
	if err != nil {
		return nil, err
	}
	fmt.Printf("%d loaded from %s\n", len(requestsFromJson), sourceFilePath)
	for i, req := range requestsFromJson {
		urlCopy := *baseRequest.Url
		requestsFromJson[i].Url = &urlCopy
		if req.UrlRaw != "" {
			url, err := url.Parse(req.UrlRaw)
			if err != nil {
				log.Fatalf("wrong url from json file: position %d, %s", i, req.UrlRaw)
			}
			requestsFromJson[i].Url = url
		}
		if req.Path != "" {
			requestsFromJson[i].Url.Path = req.Path
		}
	}

	return &FromJsonGenerator{
		baseRequest:    baseRequest,
		sourceRequests: requestsFromJson,
	}, nil
}

func (gen *FromJsonGenerator) GenerateRequests(ctx context.Context, requests chan<- *Request) {
	defer close(requests)
	for {
		randomReq := gen.sourceRequests[rand.Int()%len(gen.sourceRequests)]
		select {
		case requests <- &randomReq:
		case <-ctx.Done():
			return
		}
	}
}
