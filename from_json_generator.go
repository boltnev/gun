package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
)

type FromJsonGenerator struct {
	baseRequest    Request
	sourceRequests Requests
	// logger etc
}

type Requests = []Request

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

	return &FromJsonGenerator{
		baseRequest:    baseRequest,
		sourceRequests: requestsFromJson,
	}, nil
}

func (gen *FromJsonGenerator) GenerateRequests(ctx context.Context, requests chan<- *http.Request) {
	defer close(requests)
	baseUrl := gen.baseRequest.Url
	parsedUrl, err := url.Parse(baseUrl)
	if err != nil {
		log.Fatalf("url is not valid: %s", parsedUrl)
	}

out:
	for {
		randomReq := gen.sourceRequests[rand.Int()%len(gen.sourceRequests)]
		newUrl := parsedUrl
		method := gen.baseRequest.Method
		body := bytes.NewBuffer([]byte(gen.baseRequest.Body))
		if randomReq.Url != "" {
			// TODO: check url validity on init
			method = randomReq.Url
		}
		if randomReq.Path != "" {
			newUrl.Path = randomReq.Path
		}
		if randomReq.Method != "" {
			method = randomReq.Method
		}
		if randomReq.Body != "" {
			body = bytes.NewBuffer([]byte(randomReq.Body))
		}
		// fmt.Printf("generated: %s, %s, %d\n", method, newUrl.String(), len(randomReq.Body))
		req, err := http.NewRequest(
			method,
			newUrl.String(),
			body,
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
}
