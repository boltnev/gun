# TODO

next steps:

- [x] ability to make requests with body
    - [x] body from file
    - [x] headers
    - [x] cookies
    - [x] format for body from file
- [x] ability to make prepared requests from file
- [x] latency count
    - [x] min/avg/max + p50/p75/p90/p95/p99/p99.9 for full response time, time to first byte, and response size
    - [x] online stats: streaming histogram, percentiles printed during the run
    - [x] transfer speed sent/recv, live and in the final report
- [ ] graphics and histograms
- [x] unit tests
- [x] user agent option
- [ ] better display formats
    - [x] in-place progress redraw on terminals
    - [x] online status counts in the progress block
    - [x] three-section report: start params, live metrics table, final summary
    - [x] progress bar for the remaining test time
- [ ] performance tuning to be as good as wrk
