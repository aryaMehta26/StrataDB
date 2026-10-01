"""Validate the complete benchmark matrix before publishing a report."""
import json
import math
from pathlib import Path


def load_results(path):
    rows = [json.loads(line) for line in Path(path).read_text().splitlines()]
    expected = {(e, s, w) for e in ("stratadb", "bbolt")
                for s in (True, False) for w in "ABCW"}
    seen = set()
    settings = set()
    for row in rows:
        if type(row["sync"]) is not bool:
            raise ValueError("sync must be a boolean")
        key = row["engine"], row["sync"], row["workload"]
        if key not in expected or key in seen:
            raise ValueError(f"Unexpected or duplicate configuration: {key}")
        seen.add(key)
        for field in ("keys", "operations", "value_bytes"):
            if type(row[field]) is not int or row[field] <= 0:
                raise ValueError(f"{field} must be a positive integer")
        for field in ("seconds", "ops_per_second", "p50_us", "p99_us",
                      "physical_bytes", "live_bytes", "space_amplification"):
            value = row[field]
            if type(value) not in (int, float) or not math.isfinite(value) or value <= 0:
                raise ValueError(f"{field} must be finite and positive")
        if row["p50_us"] > row["p99_us"]:
            raise ValueError("p50 exceeds p99")
        for measured, calculated in (
            (row["ops_per_second"], row["operations"] / row["seconds"]),
            (row["space_amplification"], row["physical_bytes"] / row["live_bytes"]),
        ):
            if not math.isclose(measured, calculated, rel_tol=1e-9):
                raise ValueError("Derived metric disagrees with its inputs")
        settings.add(tuple(row[f] for f in ("keys", "operations", "value_bytes", "seed", "go", "platform")))
    if seen != expected:
        raise ValueError(f"Missing benchmark configurations: {expected - seen}")
    if len(settings) != 1:
        raise ValueError("Benchmark configurations use different inputs or environments")
    return rows


if __name__ == "__main__":
    load_results(Path(__file__).with_name("results.jsonl"))
    print("Validated 16 benchmark configurations")
