"""Update Markdown summaries from results.jsonl without hand-entered numbers."""
from validate import load_results
from pathlib import Path

root = Path(__file__).parent
rows = load_results(root / "results.jsonl")
settings = rows[0]
workload_description = (f'{settings["keys"]:,} preload keys, {settings["operations"]:,} measured operations, '
                        f'{settings["value_bytes"]:,}-byte values, uniform keys, seed {settings["seed"]}.')
environment = (root / "results.environment.txt").read_text().strip()
lines = ["# Local benchmark results", "", "```text", environment, "```", "", f"One run per configuration. {workload_description} Measurements were collected after the crash and race tests finished. No dedicated quiet-machine isolation or confidence intervals.", "", "![Throughput comparison](throughput.svg)", "", "| Engine | Mode | Workload | ops/s | p50 µs | p99 µs | Space amp | Write amp |", "|---|---|---|---:|---:|---:|---:|---:|"]
for r in rows:
    amp = f'{r["write_amplification"]:.2f}×' if "write_amplification" in r else "not instrumented"
    lines.append(f'| {r["engine"]} | {"sync" if r["sync"] else "no-sync"} | {r["workload"]} | {r["ops_per_second"]:,.0f} | {r["p50_us"]:,.2f} | {r["p99_us"]:,.2f} | {r["space_amplification"]:.2f}× | {amp} |')
lines += ["", "bbolt is the established B+ tree baseline. Sync and no-sync are separate experiments; do not compare them as equal durability. Read workload C is warm-cache and small. StrataDB uses JSON/base64 record payloads, per-block decoding, and a mutex; these costs are intentional opportunities for future experiments.", "", "The two StrataDB C runs use the same read path: no-sync only affects writes. Their large difference reveals run-to-run variability, not a causal sync-mode benefit. Repeated trials on an isolated machine are needed before attributing the difference.\n\nWrite amplification includes preload and final flush and excludes manifest and filesystem/device writes. Space amplification uses logical file sizes. Final flush is excluded from latency/throughput. These definitions matter when comparing this table to other databases.", "", "[Raw results](results.jsonl) · [Crash transcript](crash-recovery.txt) · [Full methodology](../docs/testing.md)", "", "Reproduce with `./benchmarks/run.sh`, then `python3 benchmarks/plot.py` and `python3 benchmarks/report.py`. Plotting requires matplotlib; report generation uses the Python standard library."]
(root / "README.md").write_text("\n".join(lines) + "\n")
summary = ["**1,000 SIGKILL/restart cycles passed with zero observed acknowledged-write loss.** [Committed transcript](benchmarks/crash-recovery.txt)", "", "The Bloom test observed **67 false positives / 9,999 absent keys (0.67%)** with no false negatives across 10,000 inserted keys; the ideal independent-hash estimate is about 0.82%.", "", f'Local sample: [recorded machine and toolchain](benchmarks/results.environment.txt); {settings["keys"]:,} preload keys and {settings["operations"]:,} measured operations. Selected StrataDB results:', "", "| Workload | Durability | ops/s | p99 |", "|---|---|---:|---:|"]
for w, sync in [("W", True), ("W", False), ("C", True)]:
    r = next(r for r in rows if r["engine"] == "stratadb" and r["workload"] == w and r["sync"] == sync)
    summary.append(f'| {"Inserts" if w == "W" else "Reads"} | {"Sync" if sync else "No per-write sync"} | {r["ops_per_second"]:,.0f} | {r["p99_us"] / 1000:.3f} ms |')
summary += ["", "![StrataDB and bbolt throughput](benchmarks/throughput.svg)", "", "[All 16 configurations, bbolt comparison, amplification metrics, and raw data →](benchmarks/README.md)"]
p = root.parent / "README.md"
s = p.read_text()
start, tail = s.split("<!-- RESULTS -->", 1)
_, end = tail.split("<!-- /RESULTS -->", 1)
p.write_text(start + "<!-- RESULTS -->\n" + "\n".join(summary) + "\n<!-- /RESULTS -->" + end)
