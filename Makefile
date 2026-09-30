.PHONY: test race vet demo bench crash fuzz check

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
