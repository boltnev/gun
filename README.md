# gun

`gun` is a command-line load-testing tool in the spirit of [wrk](https://github.com/wg/wrk): a single Go binary that drives prepared HTTP traffic against a service and reports latencies, statuses, and transfer rates live. It can replay a list of predefined requests (methods, bodies, headers, cookies) and shape the load with rate limiting. An experimental Qdrant mode exists too (see the end).

## Features

- **Prepared requests from JSON** (`-from_json`): a list of requests with method, path, body, headers, cookies, and content type; bodies can be read from files. Without a file, simple mode fires a single URL as fast as it can.
- **Load control**: concurrency (`-c`), duration (`-d`), per-request timeout (`-t`), rate limiting (`-rate`/`-burst`, token bucket; excess requests are dropped, not queued).
- **Headers**: repeatable `-H "Name: value"` applied to every request, per-request headers in the json (they override `-H`), a default User-Agent when neither sets one; `Host` is set through the header mechanism.
- **Live metrics** (every 500ms) and in the final report — min/max/avg + p50/p75/p90/p95/p99/p99.9 for three metrics:
  - `time-to-response` — full response time (until the body is drained);
  - `time-to-first-byte` — time to first byte (response headers received);
  - `full-response-size` — response body size in bytes.
- **Status and error counters** — also live.
- **Transfer speeds in both directions** (sent/recv, headers included) — live and in the totals.
- **Three-section report** (start → test → finish); on a terminal the test section is redrawn in place: a progress bar toward the end of the run and the live metrics table. When piped or redirected, only start and finish are printed, so logs stay clean.
- **Graceful stop**: Ctrl-C/SIGTERM still prints the finish section with everything collected so far.
- **Profiling**: `-cpuprofile`, `-memprofile` (a heap snapshot mid-run at `duration/2` and one at exit; the files are always flushed no matter how the run ends).

Percentiles are computed streaming, in fixed memory (a log-scaled histogram, 1000 buckets per decade); min/max/avg are exact.

## Build and run

```
make build          # → bin/gun (requires Go 1.22+)
```

Examples:

```
# simple mode: hammer one URL for 30 seconds with 8 threads
bin/gun -d 30s -c 8 -u http://localhost:8080

# prepared requests, capped at 500 rps, a header on every request
bin/gun -d 1m -c 16 -rate 500 -u http://localhost:8080 \
    -from_json examples/methods.json -H "Authorization: Bearer secret"

# with profiling of gun itself
bin/gun -d 30s -c 8 -u http://localhost:8080 -cpuprofile /tmp/cpu.prof -memprofile /tmp/mem.prof
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `-c`, `-concurrency` | `1` | number of parallel threads |
| `-d`, `-duration` | `1m` | test duration |
| `-t`, `-timeout` | `10s` | per-request timeout |
| `-u`, `-url` | `http://localhost` | base URL (used directly in simple mode, as the base for `-from_json`) |
| `-from_json` | — | file with the list of prepared requests |
| `-rate` | `0` | rate limit, requests per second |
| `-burst` | `= -rate` | token bucket burst size |
| `-H`, `-header` | — | header for every request, `"Name: value"`, repeatable |
| `-load_type` | `http` | `http` or `qdrant` |
| `-cpuprofile` | — | CPU profile file |
| `-memprofile` | — | heap profile file |
| `-maxproc` | `0` | override `GOMAXPROCS` |

## Prepared requests (`-from_json`)

The file is a JSON array of request descriptions. During the run requests are picked from the set **at random**. Fields:

| Field | Description |
|---|---|
| `url` | full request URL; overrides `-u` entirely |
| `path` | path, substituted into the base URL |
| `method` | HTTP method (defaults to the base — `GET`) |
| `body` | request body as a string |
| `body_file` | read the body from a file (path relative to the working directory; mutually exclusive with `body`) |
| `headers` | header map — overrides same-named `-H` flags |
| `cookies` | cookie map — appended to `Cookie` on top of headers |
| `content_type` | `Content-Type` unless set in `headers`; with `body_file` it defaults to the file extension's mime type |

## What to expect in the output

The output has three sections. On a terminal the test section lives between the rules and is redrawn every 500ms; when piped or redirected, only start and finish are printed (the two rules end up adjacent — that is expected).

The test section on a terminal looks like this:

```
  running      1.50s/2.00s  [██████████████████░░░░░░]  75%
  responses    15 224   rps 10150.4   errors 0
  statuses     200=15224
  transfer     sent 166.1KB/s   recv 1.1MB/s
         time-to-response time-to-first-byte full-response-size
  min             92.05µs            80.04µs                72B
  ...
```

Full report (piped, with `-cpuprofile`/`-memprofile`):

```
gun — http load test
  url          http://127.0.0.1:8138
  concurrency  4
  duration     2s
  timeout      10s
  requests     4 prepared from examples/methods.json
  started      2026-09-26 18:06:37
───────────────────────────────────────────────────────────────
───────────────────────────────────────────────────────────────
  finished     2026-09-26 18:06:39   elapsed 2.00s
  responses    29 827   rps 14921.6   errors 0
  statuses     200=29827

         time-to-response time-to-first-byte full-response-size
  min             92.05µs            80.04µs                72B
  max            122.53ms           122.51ms                72B
  avg            228.76µs           212.19µs                72B
  p50            192.97µs           177.21µs                72B
  p75            230.41µs           212.57µs                72B
  p90            288.07µs           268.23µs                72B
  p95            337.68µs           317.32µs                72B
  p99            500.61µs           478.08µs                72B
  p99.9            1.41ms             1.36ms                72B

  sent         3.24MB total   1.62MB/s
  recv         5.38MB total   2.69MB/s

  cpu profile  /tmp/gun_cpu.prof
  mem profile  /tmp/gun_mem.prof
```

Failed requests (timeouts, connection refused) are excluded from percentiles — they have no "full response"; they show up in `errors` and in the statuses as `0=N`. A timed-out request arrives with status 0.

## Examples

Files in `examples/` (run from the repository root — `body_file` paths are working-directory-relative):

| File | What it demonstrates |
|---|---|
| `simple_get.json` | a mix of GETs across paths, one missing — shows the 404 counter |
| `methods.json` | different methods (GET/POST/PUT/DELETE) with JSON bodies and `content_type` |
| `payload_headers.json` | per-request headers (api key, custom User-Agent) |
| `payload_body_file.json` | bodies from files + cookies + explicit `content_type` |
| `body_file_mime.json` | `body_file` without `content_type` — the type comes from the extension (`.json`, `.xml`) |
| `url_override.json` | per-request absolute `url` and a `Host` override via the header |
| `payload.json` | a large real-world set of uniform POSTs (an actual measurement) |
| `qdrant_payload.json` | requests for the experimental qdrant mode |

Bodies for the `body_file` examples live next to them: `document.json`, `query.json`, `feed.xml`.

## Qdrant (experimental)

There is a gRPC vector-search mode — it only works with `-from_json`:

```
bin/gun -load_type qdrant -d 30s -c 8 -u http://qdrant-host:6334 \
    -from_json examples/qdrant_payload.json
```

Mode limitations: unary requests only (no streaming), headers and cookies are not applied, and of the metrics table only `time-to-response` is available; instead of ttfb/size the report carries `points avg` / `max score avg`. Json schema: `collection`, `query` (vector), `params` (`hnsw_ef`, `quantization`, `indexed_only`, `exact`), `filter` (`must` + `match`/`integer`), `limit`, `with_payload`, `with_vectors`, `shard_keys`.

## Development

- Tests: `go test .` (root package), `go test ./bench` (cgo clock_gettime benchmarks, needs a C compiler).
- Checks: `gofmt`, `go vet ./...`.
- Roadmap: [TODO.md](TODO.md).

## License

MIT — see [LICENSE](LICENSE).
