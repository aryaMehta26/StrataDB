# Local benchmark results

```text
Darwin arm64
go version go1.26.5 darwin/arm64
Apple M4
15.6.1
```

One run per configuration. 2,000 preload keys, 5,000 measured operations, 256-byte values, uniform keys, seed 2027. Measurements were collected after the crash and race tests finished. No dedicated quiet-machine isolation or confidence intervals.

![Throughput comparison](throughput.svg)

| Engine | Mode | Workload | ops/s | p50 µs | p99 µs | Space amp | Write amp |
|---|---|---|---:|---:|---:|---:|---:|
| stratadb | sync | A | 370 | 3,014.67 | 11,106.04 | 2.94× | 2.76× |
| stratadb | sync | B | 3,027 | 70.88 | 5,055.54 | 1.70× | 2.98× |
| stratadb | sync | C | 17,833 | 54.33 | 93.92 | 1.52× | 2.99× |
| stratadb | sync | W | 250 | 3,919.96 | 12,564.04 | 1.52× | 2.99× |
| stratadb | no-sync | A | 25,328 | 5.54 | 242.92 | 2.94× | 2.76× |
| stratadb | no-sync | B | 7,134 | 61.38 | 1,766.21 | 1.70× | 2.98× |
| stratadb | no-sync | C | 2,598 | 101.92 | 3,531.96 | 1.52× | 2.99× |
| stratadb | no-sync | W | 32,100 | 3.62 | 79.54 | 1.52× | 2.99× |
| bbolt | sync | A | 258 | 5,887.29 | 12,056.71 | 3.91× | not instrumented |
| bbolt | sync | B | 2,924 | 2.83 | 6,959.46 | 3.91× | not instrumented |
| bbolt | sync | C | 1,082,798 | 0.67 | 2.17 | 3.91× | not instrumented |
| bbolt | sync | W | 152 | 6,088.42 | 9,926.25 | 4.47× | not instrumented |
| bbolt | no-sync | A | 48,023 | 18.08 | 157.96 | 3.91× | not instrumented |
| bbolt | no-sync | B | 454,380 | 0.71 | 23.50 | 3.91× | not instrumented |
| bbolt | no-sync | C | 1,030,335 | 0.67 | 2.21 | 3.91× | not instrumented |
| bbolt | no-sync | W | 35,046 | 23.75 | 96.75 | 4.47× | not instrumented |

bbolt is the established B+ tree baseline. Sync and no-sync are separate experiments; do not compare them as equal durability. Read workload C is warm-cache and small. StrataDB uses JSON/base64 record payloads, per-block decoding, and a mutex; these costs are intentional opportunities for future experiments.

The two StrataDB C runs use the same read path: no-sync only affects writes. Their large difference reveals run-to-run variability, not a causal sync-mode benefit. Repeated trials on an isolated machine are needed before attributing the difference.

Write amplification includes preload and final flush and excludes manifest and filesystem/device writes. Space amplification uses logical file sizes. Final flush is excluded from latency/throughput. These definitions matter when comparing this table to other databases.

[Raw results](results.jsonl) · [Crash transcript](crash-recovery.txt) · [Full methodology](../docs/testing.md)

Reproduce with `./benchmarks/run.sh`, then `python3 benchmarks/plot.py` and `python3 benchmarks/report.py`. Plotting requires matplotlib; report generation uses the Python standard library.
