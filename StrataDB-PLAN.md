# StrataDB: an LSM-tree storage engine in Go with S3-tiered storage

> **Implementation status:** This document is the original roadmap, not a list of completed features.
> See [README](README.md) for verified behavior and [architecture](docs/architecture.md) for tradeoffs.
> The initial engine implements WAL recovery, skip-list memtables, indexed SSTables, Bloom filters,
> synchronous full-merge compaction, explicit S3 tiering, a memory LRU, benchmarks, and a crash harness.
> Background leveled compaction, automatic cold-level tiering, disk cache, and live cloud measurements remain future work.
> The comparisons below are motivation only: StrataDB does not reproduce proprietary AWS database architectures.

**One line for your GitHub:** A log-structured key-value storage engine in Go, with write-ahead logging,
crash recovery, bloom filters, leveled compaction, and automatic tiering of cold data to Amazon S3,
benchmarked with YCSB-style workloads.

---

## Why this project

| Your resume today | What this adds |
|---|---|
| You've **used** Postgres, Cassandra, Neo4j, Redis, and MongoDB | You've **built** a storage engine: the internals DB teams interview on |
| CloudGate proves distributed-systems depth | This proves **storage** depth: durability, compaction, read/write amplification |
| No AWS-native storage work | S3 tiering means real AWS SDK work and a cloud cost/latency tradeoff |

It's also the **same architecture as DynamoDB, Cassandra, RocksDB, and parts of Aurora's storage**. In an AWS
Database interview, "I built an LSM engine and measured its write amplification" goes much further than
"I used DynamoDB."

It fits your strengths: it's **written in Go** like CloudGate, and like CloudGate the numbers will be committed
to the repo. Running it costs **almost nothing**: S3 storage is fractions of a cent, and you develop against
MinIO locally for free.

---

## Architecture

```
          Put / Get / Delete / Scan
                     │
        ┌────────────▼─────────────┐
        │  Write-Ahead Log (WAL)   │  append + fsync → durability
        └────────────┬─────────────┘
                     │
        ┌────────────▼─────────────┐
        │  MemTable (skip list)    │  in-memory, sorted
        └────────────┬─────────────┘
                     │ flush when full (e.g. 4 MB)
        ┌────────────▼─────────────┐
        │  L0 SSTables  (local)    │  immutable sorted files
        │  L1 SSTables  (local)    │  + bloom filter + sparse index each
        │  L2 SSTables  (local)    │
        └────────────┬─────────────┘
                     │ compaction pushes cold levels down
        ┌────────────▼─────────────┐
        │  L3+ SSTables  → S3      │  cold tier, fetched on demand,
        └──────────────────────────┘  cached locally (LRU block cache)
```

**Read path:** MemTable → L0 (newest first) → L1… For each SSTable, check the **bloom filter** first (skip the
file if the key is definitely absent), then use the **sparse index** to read a single block. S3-tier blocks go
through a local LRU cache.

**Write path:** WAL append (fsync) → MemTable insert → background flush → background compaction.

---

## Components, in build order

### 1. MemTable (skip list)
- Sorted in-memory map: Put, Get, Delete (tombstone), ordered iterator.
- Thread-safe with an `RWMutex` (start simple; lock-free is a stretch goal).

### 2. Write-Ahead Log
- Append-only file. Each record: `[CRC32][length][op][key][value]`.
- `fsync` per write in "sync" mode; batch fsync in "fast" mode. **Measure both.**
- On startup, **replay the WAL** into a fresh MemTable. The CRC detects a torn last record, which gets truncated.

### 3. SSTable format
- Data blocks (~4 KB) of sorted key/value pairs
- Sparse index: first key of each block → offset
- Bloom filter (10 bits/key ≈ 1% false positive rate)
- Footer: offsets + magic number + checksum
- Flush = write the MemTable out as an SSTable, then delete that WAL segment.

### 4. Leveled compaction
- L0 can have overlapping files. L1+ are non-overlapping, and each level is ~10× the previous one.
- Compaction merges overlapping files into the next level, keeping only the newest version of each key and
  dropping tombstones once safe.
- Runs in a background goroutine and must never block writes.

### 5. Manifest (crash safety for file metadata)
- Records which SSTables belong to which level. Update it atomically: write a temp file, fsync, rename.

### 6. S3 tiering (the AWS part)
- Levels ≥ N (configurable) live in S3 under `s3://bucket/stratadb/L3/<id>.sst`.
- Reads fetch **only the needed block** with an HTTP Range request, not the whole file.
- A local LRU block cache sits in front of S3.
- Use the AWS SDK for Go v2 and point it at **MinIO** locally; switch to real S3 with a config flag.

### 7. Benchmarks (the part that makes it resume-grade)
YCSB-style workloads, committed to `benchmarks/` exactly like CloudGate:

| Workload | Mix | What it shows |
|---|---|---|
| A | 50% read / 50% update | balanced |
| B | 95% read / 5% update | read path + bloom filters |
| C | 100% read | cache and index efficiency |
| W | 100% insert | write throughput, WAL cost |

**Report:** ops/sec, p50/p99 latency, **write amplification** (bytes written to disk ÷ bytes written by the
user), **space amplification**, and **bloom filter false-positive rate (measured vs. theoretical)**.
**Compare** against a simple baseline such as BoltDB or a plain map+file, so the numbers mean something.

**S3 tier experiment:** p99 read latency for hot local keys vs. cold S3 keys, with the cache on and off. That's
the cloud tradeoff an AWS interviewer will ask about.

### 8. Crash-recovery test (don't skip this)
A test harness that writes N keys, **kills the process at a random point** (`SIGKILL`), restarts, and asserts
every acknowledged write is still present. Run it 1,000 times in CI. "Survives 1,000 random crash-restart
cycles with zero acknowledged-write loss" is a line that holds up to any follow-up question.

---

## Repo layout

```
stratadb/
├── cmd/stratadb-bench/      # benchmark CLI
├── cmd/stratadb-server/     # optional: tiny gRPC/HTTP server in front of the engine
├── internal/memtable/       # skip list
├── internal/wal/            # write-ahead log + replay
├── internal/sstable/        # writer, reader, block format, bloom filter
├── internal/compaction/     # leveled compaction
├── internal/manifest/       # level metadata, atomic updates
├── internal/tiering/        # S3 backend + LRU block cache
├── db.go                    # public API: Open, Put, Get, Delete, Scan, Close
├── test/crash/              # kill -9 recovery harness
├── benchmarks/              # COMMITTED results (json + charts)
├── docker-compose.yml       # MinIO for local S3
└── .github/workflows/ci.yml # go test -race, crash harness, lint
```

---

## 3-week schedule (alongside DSA prep)

| Week | Build | Done when |
|---|---|---|
| **1** | MemTable, WAL + replay, SSTable writer/reader, flush | Put/Get works across restarts |
| **2** | Bloom filters, sparse index, leveled compaction, manifest, **crash harness** | 1,000 kill -9 cycles pass |
| **3** | S3 tiering + block cache, benchmark suite, README with charts | `benchmarks/` committed, README done |

**If you run short on time,** cut in this order: the server wrapper → S3 tiering → leveled compaction (fall
back to simple size-tiered). **Never cut the crash test or the benchmarks.** They're the whole point.

---

## Interview talking points you'll earn

- Why an LSM tree instead of a B-tree? Write-heavy workloads; the cost is read and space amplification.
- What does your bloom filter buy? Show measured I/O saved on workload C.
- How do you guarantee durability? WAL + fsync + CRC + atomic manifest rename, proven by the crash harness.
- What happens if S3 is slow or unavailable? Cache behavior, timeouts, and your measured p99 for cold keys.
- How would you make it distributed? Partition by key range and replicate with Raft. That's a bridge to CloudGate.

---

## Resume bullets: fill in only after you've measured

Nothing goes on the resume until the repo and `benchmarks/` exist. Then the bullets look like this, **with your
real numbers**:

- Built an LSM-tree storage engine in Go with a write-ahead log, skip-list MemTable, SSTables with bloom
  filters and sparse indexes, and leveled compaction; sustained **[N] writes/sec** at p99 **[X] ms** (YCSB
  workload W), benchmarks committed to the repo.
- Proved durability with a crash-recovery harness that survived **[1,000]** random `kill -9` restarts with zero
  acknowledged-write loss; measured write amplification of **[W]×** and bloom-filter false-positive rate of **[F]%**.
- Tiered cold levels to Amazon S3 using ranged block reads and an LRU cache, measuring p99 read latency of
  **[hot] ms** local vs **[cold] ms** from S3.

---

## One rule

Use Claude Code as much as you like while building (Amazon wants AI-assisted development). But **understand
every line you commit.** A database-team interviewer will ask you to whiteboard compaction or WAL replay
from memory. If you can do that, this project is a strong one.
