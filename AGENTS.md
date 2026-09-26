# AGENTS.md

## What this is

`gun` — a wrk-like load-testing tool (single Go binary) that can drive plain HTTP traffic or Qdrant vector-search queries. `README.md` is not docs; it is a roadmap checklist — tick items off there when a feature is done.

## Layout

- Repo root is `package main` (no `cmd/`/`internal/`): all app source files live at the root.
- `bench/` — separate `bench` package with **cgo** (clock_gettime benchmarks, `clock_test.go`). Not part of the binary; needs a C compiler; run its tests with `go test ./bench`.
- `examples/payload.json` (HTTP), `examples/payload_body_file.json` (HTTP, body from files + cookies) and `examples/qdrant_payload.json` (Qdrant) — sample inputs for the `-from_json` flag.

## Commands

- Build: `make build` → `bin/gun`. Note the Makefile compiles only root `*.go` files, deliberately excluding `bench/`; `go build ./...` will also build the cgo package.
- Tests: root package — `go test .` (HTTP mode: generators, runner via `httptest`, collector; Qdrant mode: generator protobuf mapping, runner against a fake gRPC `PointsServer`, collector). `bench` — `go test ./bench` (needs a C compiler). Root tests override the package-level globals, restoring them via `t.Cleanup`.
- No lint config; `go vet ./...` is the only check available.
- Profiling flags: `-cpuprofile`, `-memprofile` (heap profile written mid-run, at `duration/2`).

## Architecture

Pipeline wired in `main.go`, all stages communicate via channels:

```
RequestGenerator → (optional TokenBucketRateLimiter) → Runner × concurrency → Collector
```

- The four interfaces (`RequestGenerator`, `Runner`, `Collector`, `RateLimiter`) are defined in `main.go`.
- Generators close the requests channel on `ctx.Done()`; the results channel is buffered at `ResultBufferSize` (20 MB).
- Load types: `-load_type http|qdrant`. Each type has its own generator + runner pair (`FromJsonGenerator`/`HttpRunner`, `QdrantFromJsonGenerator`/`QdrantRunner`). Qdrant requests carry a protobuf `*qdrant.QueryPoints` inside `Request.AnyData`.
- `collector.go` switches on the global `loadType` for qdrant-specific stats (points found, max score); there is a TODO to refactor this.

## Conventions and gotchas

- Configuration is package-level globals. Flags are registered in `init()` in `main.go` but parsed at the top of `main()` — parsing in `init()` breaks `go test` (the test binary passes `-test.*` flags unknown at init time). New flags are registered in `init()`.
- Adding a new load type requires touching four places: the allowed-types list in `main()`, the generator switch and the runner switch in `main.go`, and the stats switches in `collector.go`.
- Bad input in generators/runners is handled with `log.Fatalf`, not error returns. Nuance: generator constructors validate per-request input and return errors, which `main()` then `log.Fatalf`s (invalid shard keys included); only unexpected types inside the hot path (e.g. wrong `AnyData`) fatal directly. Note: `encoding/json` unmarshals json numbers into `any` as `float64`, so numeric shard keys are matched on `float64`, not `int`.
- `-burst` defaults to the `-rate` value when left at 0.
- `Request` JSON schema (`from_json`): `url`, `path`, `host`, `method`, `body`, `body_file` (read into `body` at load time; working-dir-relative; errors when combined with `body`), `headers` (name→value map), `cookies` (name→value map, appended to the `Cookie` header, sorted by name), `content_type` (fills `Content-Type` unless `headers` sets it; with `body_file` it falls back to `mime.TypeByExtension`); runtime fields are tagged `json:"-"` and inherited from the `-url` base request. See `examples/payload_body_file.json`.
- HTTP headers: repeatable `-H "Name: value"` (alias `-header`) flags apply to every request; per-request `headers` from the json override them; the default user agent `Gun LoadTesting Tool/0.1` only applies when neither sets one. A `Host` header goes to `http.Request.Host` (net/http ignores it in the header map). Applied in `applyHeaders` in `http_runner.go`; qdrant/gRPC mode ignores headers. Per-request `cookies` are appended after headers via `applyCookies`.
- Qdrant JSON schema (`qdrant_from_json_generator.go`): `collection`, `query` (float vector), `params` (`hnsw_ef`, `quantization`, `indexed_only`, `exact`), `filter` (only `must` + `match`/`integer` conditions are supported), `limit`, `with_payload`, `with_vectors`, `shard_keys` (ints or strings).
- Rate limiting (`-rate`) is drop-based: `TokenBucketRateLimiter` skips requests when `Allow()` is false rather than blocking, so throttling reduces throughput, it does not queue.
