# Public website performance admission

Date: 2026-09-14

Verdict: no optimization admitted. The fixed server-rendered page is already
far below a millisecond in the in-process harness. Pre-rendering variants could
reduce allocations, but it would add cache construction and error state for no
measured user-visible bottleneck.

Command on development with Go 1.26.6:

```sh
go test -mod=readonly -run '^$' -bench '^BenchmarkRoutes$' \
  -benchmem -count=20 -benchtime=500x ./internal/site
```

Raw evidence: `.worker-evidence/web-benchmark.txt`, SHA-256
`97d6b69875aca367c506a75aff2174b1432ed44ac27c637c1112b9971acc3724`.

Nearest-rank distributions from 20 matched samples:

| Route workload | p50 | p95 | p99 | p50 throughput | p50 bytes/op | p50 allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| health | 5.970 us | 6.481 us | 6.746 us | 167,504/s | 1,264 | 17 |
| principle fragment | 15.328 us | 16.641 us | 17.182 us | 65,240/s | 3,869 | 39 |
| complete home page | 97.271 us | 101.272 us | 102.176 us | 10,281/s | 49,423 | 147 |
| CSS asset | 32.097 us | 36.253 us | 36.301 us | 31,156/s | 25,795 | 18 |
| HTMX asset | 54.180 us | 58.169 us | 60.394 us | 18,457/s | 58,562 | 18 |

The harness includes `httptest.ResponseRecorder` allocation and full body
copying, so it is a regression baseline rather than a network-capacity claim.
There is no before/after optimization and therefore no Amdahl speedup claim to
calculate. The fixed content, three-entry selector, one local render, and one
response write make the current cost model explicit; compression and Caddy are
outside this un-deployed website workstream.
