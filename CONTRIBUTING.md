# Contributing

Start with [architecture and invariants](docs/architecture.md), then run `make check`. Changes to WAL, manifest publication, tombstone handling, or remote publication must include a regression test for the affected failure mode. Keep benchmark parameters and raw results together; do not replace measurements with estimates.

Explain what can fail between each persistence step in storage-related pull requests. A benchmark improvement must preserve correctness and compare identical durability settings. Small, reviewable changes are preferred.

This project was developed with AI assistance. Generated code must be inspected and validated like any other contribution. Tests, reproducible commands, and explicit limitations support review; they do not substitute for understanding the implementation.
