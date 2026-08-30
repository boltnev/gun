package main

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakePointsServer struct {
	qdrant.UnimplementedPointsServer
	mu       sync.Mutex
	received []*qdrant.QueryPoints
	respond  func(ctx context.Context) (*qdrant.QueryResponse, error)
}

func (s *fakePointsServer) Query(ctx context.Context, q *qdrant.QueryPoints) (*qdrant.QueryResponse, error) {
	s.mu.Lock()
	s.received = append(s.received, q)
	s.mu.Unlock()
	return s.respond(ctx)
}

func (s *fakePointsServer) receivedQueries() []*qdrant.QueryPoints {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*qdrant.QueryPoints{}, s.received...)
}

// startFakeQdrant runs a gRPC server implementing the qdrant Points service
// on a random local port and returns it with a dsn for NewQdrantRunner.
func startFakeQdrant(t *testing.T, respond func(ctx context.Context) (*qdrant.QueryResponse, error)) (*fakePointsServer, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %s", err)
	}
	fake := &fakePointsServer{respond: respond}
	srv := grpc.NewServer()
	qdrant.RegisterPointsServer(srv, fake)
	t.Cleanup(srv.Stop)
	go srv.Serve(listener)
	return fake, "http://" + listener.Addr().String()
}

func newTestQdrantRunner(t *testing.T, dsn string) *QdrantRunner {
	t.Helper()
	wgReady := &sync.WaitGroup{}
	wgReady.Add(1)
	runner := NewQdrantRunner(0, wgReady, dsn)
	wgReady.Wait()
	return runner
}

func runQdrantRunnerOnce(t *testing.T, runner *QdrantRunner, query *qdrant.QueryPoints) Result {
	t.Helper()
	requests := make(chan *Request, 1)
	results := make(chan Result, 1)
	requests <- &Request{AnyData: query}
	close(requests)

	wgDone := &sync.WaitGroup{}
	wgDone.Add(1)
	runner.Run(context.Background(), wgDone, requests, results)
	wgDone.Wait()

	return <-results
}

func TestQdrantRunnerQueriesAndReturnsPoints(t *testing.T) {
	fake, addr := startFakeQdrant(t, func(ctx context.Context) (*qdrant.QueryResponse, error) {
		return &qdrant.QueryResponse{
			Result: []*qdrant.ScoredPoint{{Score: 0.9}, {Score: 0.5}},
		}, nil
	})
	setLoadGlobals(t, LoadTypeQdrant, 1, 5*time.Second)

	runner := newTestQdrantRunner(t, addr)
	res := runQdrantRunnerOnce(t, runner, &qdrant.QueryPoints{
		CollectionName: "offers",
		Query:          qdrant.NewQuery(0.25, 0.75),
	})

	if res.err != nil {
		t.Fatalf("unexpected error: %s", res.err)
	}
	points, ok := res.AnyData.([]*qdrant.ScoredPoint)
	if !ok {
		t.Fatalf("AnyData = %T, want []*qdrant.ScoredPoint", res.AnyData)
	}
	if len(points) != 2 {
		t.Errorf("points returned = %d, want 2", len(points))
	}
	if res.Latency < 0 {
		t.Errorf("latency = %s, want non-negative", res.Latency)
	}

	received := fake.receivedQueries()
	if len(received) != 1 {
		t.Fatalf("server queries = %d, want 1", len(received))
	}
	if received[0].CollectionName != "offers" {
		t.Errorf("server saw collection %q, want %q", received[0].CollectionName, "offers")
	}
	if vec := received[0].Query.GetNearest().GetDense().GetData(); len(vec) != 2 || vec[0] != 0.25 || vec[1] != 0.75 {
		t.Errorf("server saw vector %v, want [0.25 0.75]", vec)
	}
}

func TestQdrantRunnerReportsServerError(t *testing.T) {
	_, addr := startFakeQdrant(t, func(ctx context.Context) (*qdrant.QueryResponse, error) {
		return nil, errors.New("boom")
	})
	setLoadGlobals(t, LoadTypeQdrant, 1, 5*time.Second)

	runner := newTestQdrantRunner(t, addr)
	res := runQdrantRunnerOnce(t, runner, &qdrant.QueryPoints{
		CollectionName: "offers",
		Query:          qdrant.NewQuery(1),
	})

	if res.err == nil {
		t.Error("expected server error, got nil")
	}
}

func TestQdrantRunnerReportsConnectionError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %s", err)
	}
	addr := listener.Addr().String()
	listener.Close()
	dsn := "http://" + addr

	setLoadGlobals(t, LoadTypeQdrant, 1, 5*time.Second)

	runner := newTestQdrantRunner(t, dsn)
	res := runQdrantRunnerOnce(t, runner, &qdrant.QueryPoints{
		CollectionName: "offers",
		Query:          qdrant.NewQuery(1),
	})

	if res.err == nil {
		t.Error("expected connection error, got nil")
	}
}

func TestQdrantRunnerRespectsRequestTimeout(t *testing.T) {
	_, addr := startFakeQdrant(t, func(ctx context.Context) (*qdrant.QueryResponse, error) {
		select {
		case <-time.After(500 * time.Millisecond):
			return &qdrant.QueryResponse{}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	setLoadGlobals(t, LoadTypeQdrant, 1, 30*time.Millisecond)

	runner := newTestQdrantRunner(t, addr)
	res := runQdrantRunnerOnce(t, runner, &qdrant.QueryPoints{
		CollectionName: "offers",
		Query:          qdrant.NewQuery(1),
	})

	// the client wraps rpc errors into a grpc status, so check the code
	stat, ok := status.FromError(res.err)
	if !ok {
		t.Fatalf("error = %v, want grpc status error", res.err)
	}
	if stat.Code() != codes.DeadlineExceeded {
		t.Errorf("grpc code = %s, want %s", stat.Code(), codes.DeadlineExceeded)
	}
}
