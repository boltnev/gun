package main

import (
	"context"
	"testing"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

func TestNewQdrantFromJsonGeneratorErrorsOnBadSource(t *testing.T) {
	cases := map[string]string{
		"missing file":           "",
		"invalid json":           `{not a json`,
		"no collection name":     `[{"query": [1, 2]}]`,
		"no query points":        `[{"collection": "offers"}]`,
		"non-integer shard key":  `[{"collection": "c", "query": [1], "shard_keys": [3.5]}]`,
		"invalid shard key type": `[{"collection": "c", "query": [1], "shard_keys": [[1]]}]`,
	}
	for name, content := range cases {
		path := "no_such_file.json"
		if content != "" {
			path = writeTempFile(t, content)
		}
		if _, err := NewQdrantFromJsonGenerator(path); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestNewQdrantFromJsonGeneratorMapsQuery(t *testing.T) {
	path := writeTempFile(t, `[
		{
			"collection": "offers",
			"query": [0.5, -1.25],
			"params": {
				"hnsw_ef": 300,
				"indexed_only": true,
				"exact": true,
				"quantization": {"rescore": true, "oversampling": 2.5}
			},
			"filter": {
				"must": [
					{"field": "city", "value": 42, "type": "integer", "condition": "match"},
					{"field": "other", "value": 1, "type": "string", "condition": "match"}
				]
			},
			"limit": 10,
			"with_payload": true,
			"with_vectors": true,
			"shard_keys": ["shard-a", 7]
		}
	]`)
	gen, err := NewQdrantFromJsonGenerator(path)
	if err != nil {
		t.Fatalf("could not create generator: %s", err)
	}
	if len(gen.sourceRequests) != 1 {
		t.Fatalf("source requests = %d, want 1", len(gen.sourceRequests))
	}
	q, ok := gen.sourceRequests[0].AnyData.(*qdrant.QueryPoints)
	if !ok {
		t.Fatalf("AnyData = %T, want *qdrant.QueryPoints", gen.sourceRequests[0].AnyData)
	}

	if q.CollectionName != "offers" {
		t.Errorf("collection = %q, want %q", q.CollectionName, "offers")
	}
	vec := q.Query.GetNearest().GetDense().GetData()
	if len(vec) != 2 || vec[0] != 0.5 || vec[1] != -1.25 {
		t.Errorf("query vector = %v, want [0.5 -1.25]", vec)
	}

	if q.Params == nil {
		t.Fatal("params not mapped")
	}
	if q.Params.GetHnswEf() != 300 {
		t.Errorf("hnsw_ef = %d, want 300", q.Params.GetHnswEf())
	}
	if !q.Params.GetIndexedOnly() {
		t.Error("indexed_only = false, want true")
	}
	if !q.Params.GetExact() {
		t.Error("exact = false, want true")
	}
	if !q.Params.GetQuantization().GetRescore() {
		t.Error("quantization rescore = false, want true")
	}
	if q.Params.GetQuantization().GetOversampling() != 2.5 {
		t.Errorf("quantization oversampling = %v, want 2.5", q.Params.GetQuantization().GetOversampling())
	}

	// only match+integer conditions are mapped; other combos are dropped
	must := q.Filter.GetMust()
	if len(must) != 1 {
		t.Fatalf("filter must conditions = %d, want 1", len(must))
	}
	if key := must[0].GetField().GetKey(); key != "city" {
		t.Errorf("filter field = %q, want %q", key, "city")
	}
	if value := must[0].GetField().GetMatch().GetInteger(); value != 42 {
		t.Errorf("filter value = %d, want 42", value)
	}

	if q.GetLimit() != 10 {
		t.Errorf("limit = %d, want 10", q.GetLimit())
	}
	if !q.WithPayload.GetEnable() {
		t.Error("with_payload not enabled")
	}
	if !q.WithVectors.GetEnable() {
		t.Error("with_vectors not enabled")
	}

	shardKeys := q.GetShardKeySelector().GetShardKeys()
	if len(shardKeys) != 2 {
		t.Fatalf("shard keys = %d, want 2", len(shardKeys))
	}
	if shardKeys[0].GetKeyword() != "shard-a" {
		t.Errorf("shard key 0 = %q, want %q", shardKeys[0].GetKeyword(), "shard-a")
	}
	if shardKeys[1].GetNumber() != 7 {
		t.Errorf("shard key 1 = %d, want 7", shardKeys[1].GetNumber())
	}
}

func TestNewQdrantFromJsonGeneratorMinimalRequest(t *testing.T) {
	path := writeTempFile(t, `[{"collection": "offers", "query": [1, 2, 3]}]`)
	gen, err := NewQdrantFromJsonGenerator(path)
	if err != nil {
		t.Fatalf("could not create generator: %s", err)
	}
	q, ok := gen.sourceRequests[0].AnyData.(*qdrant.QueryPoints)
	if !ok {
		t.Fatalf("AnyData = %T, want *qdrant.QueryPoints", gen.sourceRequests[0].AnyData)
	}
	if q.Params != nil || q.Filter != nil || q.ShardKeySelector != nil {
		t.Errorf("optional fields mapped for minimal request: params=%v filter=%v shard_keys=%v",
			q.Params != nil, q.Filter != nil, q.ShardKeySelector != nil)
	}
}

func TestQdrantFromJsonGeneratorLoadsExamplePayload(t *testing.T) {
	if _, err := NewQdrantFromJsonGenerator("examples/qdrant_payload.json"); err != nil {
		t.Errorf("could not load example payload: %s", err)
	}
}

func TestQdrantFromJsonGeneratorGenerateRequests(t *testing.T) {
	path := writeTempFile(t, `[
		{"collection": "one", "query": [1]},
		{"collection": "two", "query": [2]}
	]`)
	gen, err := NewQdrantFromJsonGenerator(path)
	if err != nil {
		t.Fatalf("could not create generator: %s", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	requests := make(chan *Request)
	go gen.GenerateRequests(ctx, requests)

	allowedCollections := map[string]bool{"one": true, "two": true}
	seenCollections := map[string]bool{}
	const samples = 20
	for range samples {
		select {
		case req := <-requests:
			q, ok := req.AnyData.(*qdrant.QueryPoints)
			if !ok {
				t.Fatalf("AnyData = %T, want *qdrant.QueryPoints", req.AnyData)
			}
			if !allowedCollections[q.CollectionName] {
				t.Errorf("generated collection %q, want one of one, two", q.CollectionName)
			}
			seenCollections[q.CollectionName] = true
		case <-time.After(2 * time.Second):
			t.Fatal("generator stalled")
		}
	}
	if len(seenCollections) < 2 {
		t.Errorf("generator picked only %d distinct collections over %d samples, want random choice", len(seenCollections), samples)
	}

	// after cancel the generator may still win the select race and deliver a
	// few more requests; the guarantee is that the channel gets closed
	cancel()
	closedDeadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-requests:
			if !ok {
				return
			}
		case <-closedDeadline:
			t.Fatal("requests channel was not closed after cancel")
		}
	}
}
