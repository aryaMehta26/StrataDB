#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
binary=$(mktemp)
trap 'rm -f "$binary"' EXIT
go build -o "$binary" ./cmd/stratadb-bench
out=${1:-benchmarks/results.jsonl}
mkdir -p "$(dirname "$out")"
{
  uname -sm
  go version
  if [[ "$(uname -s)" == Darwin ]]; then
    sysctl -n machdep.cpu.brand_string
    sw_vers -productVersion
  fi
} > "${out%.jsonl}.environment.txt"
: > "$out"
for engine in stratadb bbolt; do
  for mode in sync no-sync; do
    args=()
    if [[ "$mode" == no-sync ]]; then args+=("-no-sync"); fi
    for workload in A B C W; do
      "$binary" -engine "$engine" -workload "$workload" ${args[@]+"${args[@]}"} >> "$out"
    done
  done
done
