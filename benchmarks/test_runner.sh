#!/usr/bin/env bash
# Ensure a failed benchmark never truncates previously published evidence.
set -euo pipefail
cd "$(dirname "$0")/.."
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
mkdir "$fixture/bin"
cat > "$fixture/bin/go" <<'MOCK'
#!/usr/bin/env bash
if [[ "$1" == build ]]; then
  printf '#!/usr/bin/env bash\nexit 42\n' > "$3"
  chmod +x "$3"
else
  printf 'test toolchain\n'
fi
MOCK
chmod +x "$fixture/bin/go"
printf 'previous results\n' > "$fixture/results.jsonl"
printf 'previous environment\n' > "$fixture/results.environment.txt"
if PATH="$fixture/bin:$PATH" bash benchmarks/run.sh "$fixture/results.jsonl"; then
  echo 'Expected benchmark failure' >&2
  exit 1
fi
[[ "$(cat "$fixture/results.jsonl")" == 'previous results' ]]
[[ "$(cat "$fixture/results.environment.txt")" == 'previous environment' ]]
[[ -z "$(find "$fixture" -name '*.tmp.*' -print)" ]]
echo 'Failed runs preserve existing benchmark evidence'
