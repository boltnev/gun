package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"

	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/protobuf/proto"
)

type QdrantFromJsonGenerator struct {
	sourceRequests Requests
	// logger etc
}

type QdrantQueryParamsQuantization struct {
	Rescore      bool    `json:"rescore,omitempty"`
	Oversampling float64 `json:"oversampling,omitempty"`
}

type QdrantQueryFilterMustCondition struct {
	Field     string `json:"field"`
	Value     int    `json:"value"`
	Type      string `json:"type"`
	Condition string `json:"condition"`
}

type QdrantQueryFilter struct {
	Must []QdrantQueryFilterMustCondition `json:"must,omitempty"`
}

type QdrantQueryParams struct {
	HnswEf       int                            `json:"hnsw_ef,omitempty"`
	Quantization *QdrantQueryParamsQuantization `json:"quantization,omitempty"`
	IndexedOnly  bool                           `json:"indexed_only,omitempty"`
	Exact        bool                           `json:"exact,omitempty"`
}

type QdrantQuery struct {
	Collection  string             `json:"collection"`
	Query       []float32          `json:"query"`
	Params      *QdrantQueryParams `json:"params,omitempty"`
	Filter      *QdrantQueryFilter `json:"filter,omitempty"`
	Limit       int                `json:"limit,omitempty"`
	WithPayload bool               `json:"with_payload,omitempty"`
	WithVectors bool               `json:"with_vectors,omitempty"`
	ShardKeys   []uint64           `json:"shard_keys"`
}

type QdrantQueries []*QdrantQuery

func NewQdrantFromJsonGenerator(sourceFilePath string) (*QdrantFromJsonGenerator, error) {
	qdrantQueriesFromJson := QdrantQueries{}

	sourceBytes, err := os.ReadFile(sourceFilePath)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(sourceBytes, &qdrantQueriesFromJson)
	if err != nil {
		return nil, err
	}
	requestsQdrantFromJson := make(Requests, 0, len(qdrantQueriesFromJson))

	for i, req := range qdrantQueriesFromJson {
		qdrantRequest := &qdrant.QueryPoints{}
		if req.Collection == "" {
			return nil, fmt.Errorf("wrong request %d: no collection name", i)
		}
		qdrantRequest.CollectionName = req.Collection
		if len(req.Query) == 0 {
			return nil, fmt.Errorf("wrong request %d: no query points", i)
		}
		qdrantRequest.Query = qdrant.NewQuery(req.Query...)
		if req.Filter != nil {
			filter := qdrant.Filter{}
			if req.Filter.Must != nil {
				for _, condition := range req.Filter.Must {
					if condition.Condition == "match" && condition.Type == "integer" {
						filter.Must = append(filter.Must,
							qdrant.NewMatchInt(condition.Field, int64(condition.Value)),
						)
					}
				}
			}
			qdrantRequest.Filter = &filter
		}
		if req.Params != nil {
			params := qdrant.SearchParams{}
			if req.Params.HnswEf != 0 {
				params.HnswEf = proto.Uint64(uint64(req.Params.HnswEf))
			}
			if req.Params.Quantization != nil {
				params.Quantization = &qdrant.QuantizationSearchParams{
					Rescore:      proto.Bool(req.Params.Quantization.Rescore),
					Oversampling: proto.Float64(req.Params.Quantization.Oversampling),
				}
			}

			if req.Params.IndexedOnly {
				params.IndexedOnly = proto.Bool(req.Params.IndexedOnly)
			}
			if req.Params.Exact {
				params.Exact = proto.Bool(req.Params.Exact)
			}

			qdrantRequest.Params = &params
		}
		if req.WithPayload {
			qdrantRequest.WithPayload = qdrant.NewWithPayload(req.WithPayload)
		}

		if req.WithVectors {
			qdrantRequest.WithVectors = qdrant.NewWithVectors(req.WithVectors)
		}

		if req.Limit != 0 {
			qdrantRequest.Limit = proto.Uint64(uint64(req.Limit))
		}

		if len(req.ShardKeys) > 0 {
			shardKeys := []*qdrant.ShardKey{}
			for _, shardKey := range req.ShardKeys {
				shardKeys = append(shardKeys, qdrant.NewShardKeyNum(shardKey))
			}
			qdrantRequest.ShardKeySelector = &qdrant.ShardKeySelector{ShardKeys: shardKeys}
		}

		requestsQdrantFromJson = append(requestsQdrantFromJson, Request{AnyData: qdrantRequest})
	}

	fmt.Printf("%d loaded from %s\n", len(requestsQdrantFromJson), sourceFilePath)
	return &QdrantFromJsonGenerator{
		sourceRequests: requestsQdrantFromJson,
	}, nil
}

func (gen *QdrantFromJsonGenerator) GenerateRequests(ctx context.Context, requests chan<- *Request) {
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
