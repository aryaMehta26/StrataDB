package stratadb

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"
)

func openTest(t *testing.T, dir string, o Options) *DB {
	t.Helper()
	d, e := Open(dir, o)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	return d
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func TestModel(t *testing.T) {
	dir := t.TempDir()
	d := openTest(t, dir, Options{MemtableBytes: 256})
	model := map[string]string{}
	r := rand.New(rand.NewSource(42))
	for i := 0; i < 800; i++ {
		k := fmt.Sprintf("k%03d", r.Intn(80))
		if r.Intn(4) == 0 {
			must(t, d.Delete(k))
			delete(model, k)
		} else {
			v := fmt.Sprint(i)
			must(t, d.Put(k, []byte(v)))
			model[k] = v
		}
		if i%71 == 0 {
			must(t, d.Compact(false))
		}
		if i%113 == 0 {
			must(t, d.Close())
			d = openTest(t, dir, Options{MemtableBytes: 256})
		}
		for key, v := range model {
			got, e := d.Get(key)
			must(t, e)
			if string(got) != v {
				t.Fatalf("%s: got %q want %q", key, got, v)
			}
		}
	}
	rows, e := d.Scan("", "")
	must(t, e)
	actual := map[string]string{}
	for i, p := range rows {
		actual[p.Key] = string(p.Value)
		if i > 0 && rows[i-1].Key >= p.Key {
			t.Fatal("unsorted scan")
		}
	}
	if !reflect.DeepEqual(actual, model) {
		t.Fatal("model mismatch")
	}
	rows, e = d.Scan("k020", "k040")
	must(t, e)
	for _, p := range rows {
		if p.Key < "k020" || p.Key >= "k040" {
			t.Fatal("scan bounds")
		}
	}
}
func TestRecoveryCorruptionAndLock(t *testing.T) {
	dir := t.TempDir()
	d := openTest(t, dir, Options{})
	if other, e := Open(dir, Options{}); e == nil {
		other.Close()
		t.Fatal("second open succeeded")
	}
	must(t, d.Put("a", []byte("v")))
	must(t, d.Close())
	f, e := os.OpenFile(filepath.Join(dir, "WAL"), os.O_APPEND|os.O_WRONLY, 0600)
	must(t, e)
	_, e = f.Write([]byte{1, 2, 3})
	must(t, e)
	must(t, f.Close())
	d = openTest(t, dir, Options{})
	v, e := d.Get("a")
	must(t, e)
	if string(v) != "v" {
		t.Fatal("lost value")
	}
	must(t, d.Put("b", []byte("w")))
	must(t, d.Close())
	p := filepath.Join(dir, "WAL")
	b, e := os.ReadFile(p)
	must(t, e)
	b[10] ^= 1
	must(t, os.WriteFile(p, b, 0600))
	if d, e := Open(dir, Options{}); e == nil {
		d.Close()
		t.Fatal("corruption accepted")
	}
}
func TestConcurrentAndOwnership(t *testing.T) {
	d := openTest(t, t.TempDir(), Options{MemtableBytes: 2048})
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 60; i++ {
				k := fmt.Sprintf("%d/%d", g, i)
				if e := d.Put(k, []byte(k)); e != nil {
					t.Error(e)
				}
				v, e := d.Get(k)
				if e != nil || string(v) != k {
					t.Error("read mismatch")
				}
			}
		}(g)
	}
	wg.Wait()
	b := []byte("safe")
	must(t, d.Put("copy", b))
	b[0] = 'X'
	v, e := d.Get("copy")
	must(t, e)
	v[0] = 'Y'
	v, e = d.Get("copy")
	must(t, e)
	if string(v) != "safe" {
		t.Fatal("aliased value")
	}
	must(t, d.Close())
	if _, e = d.Get("x"); !errors.Is(e, ErrClosed) {
		t.Fatal(e)
	}
}

type memoryStore struct {
	objects map[string][]byte
	fail    bool
	ranges  int
}

func (s *memoryStore) Put(_ context.Context, k string, b []byte) error {
	if s.fail {
		return errors.New("offline")
	}
	s.objects[k] = append([]byte(nil), b...)
	return nil
}
func (s *memoryStore) Range(_ context.Context, k string, o int64, n int) ([]byte, error) {
	if s.fail {
		return nil, errors.New("offline")
	}
	s.ranges++
	return append([]byte(nil), s.objects[k][int(o):int(o)+n]...), nil
}
func TestRemoteCacheAndFailure(t *testing.T) {
	s := &memoryStore{objects: map[string][]byte{}}
	dir := t.TempDir()
	d := openTest(t, dir, Options{Store: s, CacheBytes: 8192})
	for i := 0; i < 100; i++ {
		must(t, d.Put(fmt.Sprintf("%04d", i), bytes.Repeat([]byte("v"), 100)))
	}
	s.fail = true
	if e := d.Compact(true); e == nil {
		t.Fatal("upload unexpectedly succeeded")
	}
	_, e := d.Get("0001")
	must(t, e)
	s.fail = false
	must(t, d.Compact(true))
	must(t, d.Close())
	d = openTest(t, dir, Options{Store: s, CacheBytes: 8192})
	_, e = d.Get("0001")
	must(t, e)
	s.fail = true
	_, e = d.Get("0001")
	must(t, e)
	if _, e = d.Get("0099"); e == nil {
		t.Fatal("expected cache miss failure")
	}
	if d.Stats().CacheHits != 1 {
		t.Fatal("cache miss")
	}
	s.fail = false
	must(t, d.Delete("0001"))
	must(t, d.Compact(true))
	if _, e = d.Get("0001"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

// TestCrashHelper is run in a child process. Each line acknowledges a durable Put.
func TestCrashHelper(t *testing.T) {
	dir := os.Getenv("STRATA_CRASH_CHILD")
	if dir == "" {
		return
	}
	d, e := Open(dir, Options{MemtableBytes: 1024})
	if e != nil {
		os.Exit(2)
	}
	for i := 0; ; i++ {
		if e = d.Put(fmt.Sprintf("%08d", i), []byte("durable")); e != nil {
			os.Exit(3)
		}
		fmt.Println(i)
		if i%17 == 0 {
			if e = d.Compact(false); e != nil {
				os.Exit(4)
			}
		}
	}
}
func TestCrashRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess crash harness")
	}
	cycles := 20
	if s := os.Getenv("STRATA_CRASH_CYCLES"); s != "" {
		n, e := strconv.Atoi(s)
		must(t, e)
		if n <= 0 {
			t.Fatal("cycles must be positive")
		}
		cycles = n
	}
	r := rand.New(rand.NewSource(2027))
	base := t.TempDir()
	for cycle := 0; cycle < cycles; cycle++ {
		dir := filepath.Join(base, strconv.Itoa(cycle))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCrashHelper$")
		cmd.Env = append(os.Environ(), "STRATA_CRASH_CHILD="+dir)
		stdout, e := cmd.StdoutPipe()
		must(t, e)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		must(t, cmd.Start())
		scanner := bufio.NewScanner(stdout)
		last := -1
		target := 1 + r.Intn(45)
		for j := 0; j < target && scanner.Scan(); j++ {
			last, e = strconv.Atoi(scanner.Text())
			if e != nil {
				break
			}
		}
		time.Sleep(time.Duration(r.Intn(500)) * time.Microsecond)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		cancel()
		if last < 0 || e != nil {
			t.Fatalf("child did not acknowledge: %v %s", e, stderr.String())
		}
		d, e := Open(dir, Options{})
		must(t, e)
		for i := 0; i <= last; i++ {
			v, e := d.Get(fmt.Sprintf("%08d", i))
			if e != nil || string(v) != "durable" {
				t.Fatalf("cycle %d lost acknowledged key %d: %v", cycle, i, e)
			}
		}
		must(t, d.Close())
		must(t, os.RemoveAll(dir))
	}
	t.Logf("%d SIGKILL/restart cycles: zero acknowledged-write loss", cycles)
}

func TestManifestAndSSTCorruption(t *testing.T) {
	for _, target := range []string{"manifest", "sst"} {
		t.Run(target, func(t *testing.T) {
			dir := t.TempDir()
			d := openTest(t, dir, Options{})
			must(t, d.Put("key", []byte("value")))
			must(t, d.Flush())
			table := d.tables[0]
			must(t, d.Close())
			path := filepath.Join(dir, "MANIFEST")
			if target == "sst" {
				path = d.path(table)
			}
			b, e := os.ReadFile(path)
			must(t, e)
			if target == "sst" {
				b[len(b)-1] ^= 1
			} else {
				b[len(b)/2] ^= 1
			}
			must(t, os.WriteFile(path, b, 0600))
			d, e = Open(dir, Options{})
			if target == "manifest" {
				if e == nil {
					d.Close()
					t.Fatal("damaged manifest accepted")
				}
				return
			}
			must(t, e)
			defer d.Close()
			if _, e = d.Get("key"); e == nil {
				t.Fatal("damaged table accepted")
			}
		})
	}
}
func TestCacheEviction(t *testing.T) {
	s := &memoryStore{objects: map[string][]byte{}}
	d := openTest(t, t.TempDir(), Options{Store: s, CacheBytes: 6000})
	for i := 0; i < 200; i++ {
		must(t, d.Put(fmt.Sprintf("%04d", i), bytes.Repeat([]byte("a"), 100)))
	}
	must(t, d.Compact(true))
	for _, k := range []string{"0000", "0199", "0000"} {
		_, e := d.Get(k)
		must(t, e)
	}
	if s.ranges != 3 {
		t.Fatalf("expected eviction: %d requests", s.ranges)
	}
	if d.cacheSize > 6000 {
		t.Fatal("cache limit exceeded")
	}
}

func TestKeyAndValueEdges(t *testing.T) {
	dir := t.TempDir()
	d := openTest(t, dir, Options{})
	if e := d.Put(string([]byte{255}), []byte("bad")); e == nil {
		t.Fatal("invalid UTF-8 key accepted")
	}
	want := []byte{0, 255, 128}
	must(t, d.Put("", want))
	must(t, d.Put("日本語", nil))
	must(t, d.Flush())
	must(t, d.Close())
	d = openTest(t, dir, Options{})
	got, e := d.Get("")
	must(t, e)
	if !bytes.Equal(got, want) {
		t.Fatal("binary value changed")
	}
	_, e = d.Get("日本語")
	must(t, e)
}

// Simulate a crash after manifest publication but before WAL retirement. Replay
// must be idempotent for overwrites and must not resurrect deleted older values.
func TestPublishedManifestWithUnretiredWAL(t *testing.T) {
	dir := t.TempDir()
	d := openTest(t, dir, Options{})
	must(t, d.Put("updated", []byte("old")))
	must(t, d.Put("deleted", []byte("old")))
	must(t, d.Flush())
	must(t, d.Put("updated", []byte("new")))
	must(t, d.Delete("deleted"))
	wal, e := os.ReadFile(filepath.Join(dir, "WAL"))
	must(t, e)
	must(t, d.Compact(false))
	must(t, d.Close())
	must(t, os.WriteFile(filepath.Join(dir, "WAL"), wal, 0600))
	must(t, os.WriteFile(filepath.Join(dir, "orphan.sst"), []byte("unpublished"), 0600))
	d = openTest(t, dir, Options{})
	got, e := d.Get("updated")
	must(t, e)
	if string(got) != "new" {
		t.Fatal("replay lost latest value")
	}
	if _, e = d.Get("deleted"); !errors.Is(e, ErrNotFound) {
		t.Fatal("deleted value resurrected", e)
	}
	must(t, d.Flush())
	must(t, d.Close())
	d = openTest(t, dir, Options{})
	if _, e = d.Get("deleted"); !errors.Is(e, ErrNotFound) {
		t.Fatal("deleted value resurrected after another checkpoint", e)
	}
}
