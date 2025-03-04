package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

type QdrantRunner struct {
	threadNum int
	client    *qdrant.Client
}

func NewQdrantRunner(threadNum int, wgReady *sync.WaitGroup, dsn string) *QdrantRunner {
	defer wgReady.Done()
	url, err := url.Parse(dsn)
	if err != nil {
		log.Fatalf("wrong dsn format %s\n", dsn)
	}

	port, err := strconv.Atoi(url.Port())
	if err != nil {
		log.Fatalf("wrong port %s\n", dsn)
	}

	client, err := qdrant.NewClient(&qdrant.Config{
		Host: url.Hostname(),
		Port: port,
	})
	if err != nil {
		log.Fatalf("could not create qdrant client%s\n", dsn)
	}
	fmt.Printf("thread %d is ready\n", threadNum)
	return &QdrantRunner{
		client:    client,
		threadNum: threadNum,
	}
}

func (h *QdrantRunner) Run(ctx context.Context, wgDone *sync.WaitGroup, requests <-chan *Request, results chan<- Result) {
	defer h.client.Close()
	defer wgDone.Done()

	for req := range requests {
		switch q := req.AnyData.(type) {
		case *qdrant.QueryPoints:
			reqCtx, cancel := context.WithTimeout(ctx, timeout)
			// do the req
			start := time.Now()
			points, err := h.client.Query(reqCtx, q)

			cancel()
			results <- Result{
				Latency: time.Since(start),
				err:     err,
				AnyData: points,
			}
		default:
			log.Fatalf("internal error: wrong request data type %T\n", q)
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}
