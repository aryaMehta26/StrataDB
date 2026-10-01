#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
binary=$(mktemp)
cleanup() {
  rm -f "$binary"
  if [[ -n "${results_tmp:-}" ]]; then rm -f "$results_tmp"; fi
  if [[ -n "${environment_tmp:-}" ]]; then rm -f "$environment_tmp"; fi
}
trap cleanup EXIT
go build -o "$binary" ./cmd/stratadb-bench
out=${1:-benchmarks/results.jsonl}
mkdir -p "$(dirname "$out")"
results_tmp=$(mktemp "${out}.tmp.XXXXXX")
environment_tmp=$(mktemp "${out%.jsonl}.environment.tmp.XXXXXX")
{
  uname -sm
  go version
  if [[ "$(uname -s)" == Darwin ]]; then
    sysctl -n machdep.cpu.brand_string
    sw_vers -productVersion
  fi
} > "$environment_tmp"
for engine in stratadb bbolt; do
  for mode in sync no-sync; do
    args=()
    if [[ "$mode" == no-sync ]]; then args+=("-no-sync"); fi
    for workload in A B C W; do
      "$binary" -engine "$engine" -workload "$workload" ${args[@]+"${args[@]}"} >> "$results_tmp"
    done
  done
done
# Keep the previous evidence intact unless every benchmark succeeds.
mv "$environment_tmp" "${out%.jsonl}.environment.txt"
mv "$results_tmp" "$out"
