package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type FromJsonGenerator struct {
	baseRequest    Request
	sourceRequests Requests
	// logger etc
}

// PreparedRequests is the number of requests loaded from the json file.
func (g *FromJsonGenerator) PreparedRequests() int {
	return len(g.sourceRequests)
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
	for i, req := range requestsFromJson {
		urlCopy := *baseRequest.Url
		requestsFromJson[i].Url = &urlCopy
		if req.UrlRaw != "" {
			url, err := url.Parse(req.UrlRaw)
			if err != nil {
				fatal("wrong url from json file: position %d, %s", i, req.UrlRaw)
			}
			requestsFromJson[i].Url = url
		}
		if req.Path != "" {
			requestsFromJson[i].Url.Path = req.Path
		}
		if req.BodyFile != "" {
			if req.Body != "" {
				return nil, fmt.Errorf("position %d: both body and body_file are set", i)
			}
			body, err := os.ReadFile(req.BodyFile)
			if err != nil {
				return nil, fmt.Errorf("position %d: could not read body_file %q: %s", i, req.BodyFile, err)
			}
			requestsFromJson[i].Body = string(body)
		}
		resolveContentType(&requestsFromJson[i])
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

// resolveContentType materializes Content-Type into the per-request headers:
// an explicit header wins, then the content_type field, then the body_file
// extension.
func resolveContentType(req *Request) {
	if hasHeader(req.Headers, "Content-Type") {
		return
	}
	contentType := req.ContentType
	if contentType == "" && req.BodyFile != "" {
		contentType = mime.TypeByExtension(filepath.Ext(req.BodyFile))
	}
	if contentType == "" {
		return
	}
	if req.Headers == nil {
		req.Headers = map[string]string{}
	}
	req.Headers["Content-Type"] = contentType
}

func hasHeader(headers map[string]string, name string) bool {
	for n := range headers {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}
