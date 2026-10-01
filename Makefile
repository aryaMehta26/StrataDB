.PHONY: test race vet demo bench crash fuzz check evidence

test:
	go test ./...
race:
	go test -race ./...
vet:
	go vet ./...
demo:
	go run ./cmd/stratadb-demo
bench:
	./benchmarks/run.sh
crash:
	STRATA_CRASH_CYCLES=1000 go test -run '^TestCrashRecovery$$' -v -timeout 20m .
fuzz:
	go test ./internal/storage -run '^$$' -fuzz FuzzRead -fuzztime 10s
check: vet race

# Check committed data and report consistency without rerunning timed workloads.
evidence:
	python3 -m unittest discover -s benchmarks -p 'test_*.py'
	python3 benchmarks/validate.py
	bash benchmarks/test_runner.sh
	python3 benchmarks/report.py
	git diff --exit-code -- README.md benchmarks/README.md
