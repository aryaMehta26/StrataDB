// stratadb-bench runs single-client, uniform-key, YCSB-inspired workloads.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	db "github.com/aryaMehta26/StrataDB"
	bolt "go.etcd.io/bbolt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

type result struct {
	Engine             string    `json:"engine"`
	Workload           string    `json:"workload"`
	Sync               bool      `json:"sync"`
	Keys               int       `json:"keys"`
	Operations         int       `json:"operations"`
	ValueBytes         int       `json:"value_bytes"`
	Seed               int64     `json:"seed"`
	Go                 string    `json:"go"`
	Platform           string    `json:"platform"`
	Seconds            float64   `json:"seconds"`
	OpsPerSecond       float64   `json:"ops_per_second"`
	P50us              float64   `json:"p50_us"`
	P99us              float64   `json:"p99_us"`
	PhysicalBytes      int64     `json:"physical_bytes"`
	LiveBytes          int64     `json:"live_bytes"`
	SpaceAmplification float64   `json:"space_amplification"`
	EngineStats        *db.Stats `json:"engine_stats,omitempty"`
	WriteAmplification *float64  `json:"write_amplification,omitempty"`
}

func main() {
	engine := flag.String("engine", "stratadb", "stratadb or bbolt")
	work := flag.String("workload", "A", "A/B/C/W")
	keys := flag.Int("keys", 2000, "preloaded keys")
	ops := flag.Int("ops", 5000, "measured operations")
	size := flag.Int("value-bytes", 256, "value bytes")
	noSync := flag.Bool("no-sync", false, "disable per-write synchronization")
	seed := flag.Int64("seed", 2027, "random seed")
	flag.Parse()
	if e := run(*engine, *work, *keys, *ops, *size, *noSync, *seed); e != nil {
		log.Fatal(e)
	}
}
func run(engine, work string, keys, ops, size int, noSync bool, seed int64) error {
	if keys <= 0 || ops <= 0 || size <= 0 {
		return fmt.Errorf("keys, ops, and value-bytes must be positive")
	}
	reads, ok := map[string]int{"A": 50, "B": 95, "C": 100, "W": 0}[work]
	if !ok {
		return fmt.Errorf("unknown workload %q", work)
	}
	dir, e := os.MkdirTemp("", "stratadb-bench-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	var put func(string, []byte) error
	var get func(string) ([]byte, error)
	var flush func() error
	var stats func() db.Stats
	switch engine {
	case "stratadb":
		d, e := db.Open(dir, db.Options{NoSync: noSync, MemtableBytes: 256 << 10})
		if e != nil {
			return e
		}
		defer d.Close()
		put = d.Put
		get = d.Get
		flush = d.Flush
		stats = d.Stats
	case "bbolt":
		d, e := bolt.Open(filepath.Join(dir, "bolt.db"), 0600, &bolt.Options{NoSync: noSync})
		if e != nil {
			return e
		}
		defer d.Close()
		if e = d.Update(func(tx *bolt.Tx) error { _, e := tx.CreateBucket([]byte("data")); return e }); e != nil {
			return e
		}
		put = func(k string, v []byte) error {
			return d.Update(func(tx *bolt.Tx) error { return tx.Bucket([]byte("data")).Put([]byte(k), v) })
		}
		get = func(k string) ([]byte, error) {
			var b []byte
			e := d.View(func(tx *bolt.Tx) error {
				v := tx.Bucket([]byte("data")).Get([]byte(k))
				if v == nil {
					return db.ErrNotFound
				}
				b = append([]byte(nil), v...)
				return nil
			})
			return b, e
		}
		flush = func() error { return nil }
	default:
		return fmt.Errorf("unknown engine %q", engine)
	}
	key := func(i int) string { return fmt.Sprintf("key%09d", i) }
	value := make([]byte, size)
	for i := range value {
		value[i] = byte('a' + i%26)
	}
	for i := 0; i < keys; i++ {
		if e = put(key(i), value); e != nil {
			return e
		}
	}
	if e = flush(); e != nil {
		return e
	}
	rng := rand.New(rand.NewSource(seed))
	lat := make([]int64, ops)
	start := time.Now()
	live := keys
	for i := 0; i < ops; i++ {
		read := rng.Intn(100) < reads
		k := key(rng.Intn(keys))
		if work == "W" {
			k = key(keys + i)
			live++
		}
		t := time.Now()
		if read {
			v, err := get(k)
			e = err
			if e == nil && string(v) != string(value) {
				return fmt.Errorf("read mismatch")
			}
		} else {
			e = put(k, value)
		}
		lat[i] = time.Since(t).Nanoseconds()
		if e != nil {
			return e
		}
	}
	elapsed := time.Since(start)
	if e = flush(); e != nil {
		return e
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	var physical int64
	e = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			s, e := entry.Info()
			if e != nil {
				return e
			}
			physical += s.Size()
		}
		return nil
	})
	if e != nil {
		return e
	}
	liveBytes := int64(live * (12 + size))
	r := result{Engine: engine, Workload: work, Sync: !noSync, Keys: keys, Operations: ops, ValueBytes: size, Seed: seed, Go: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH, Seconds: elapsed.Seconds(), OpsPerSecond: float64(ops) / elapsed.Seconds(), P50us: float64(lat[(ops-1)/2]) / 1000, P99us: float64(lat[(ops-1)*99/100]) / 1000, PhysicalBytes: physical, LiveBytes: liveBytes, SpaceAmplification: float64(physical) / float64(liveBytes)}
	if stats != nil {
		s := stats()
		r.EngineStats = &s
		if s.UserBytes > 0 {
			a := float64(s.WALBytes+s.TableBytes) / float64(s.UserBytes)
			r.WriteAmplification = &a
		}
	}
	return json.NewEncoder(os.Stdout).Encode(r)
}
