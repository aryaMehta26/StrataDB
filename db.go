// Package stratadb provides an embedded, single-writer LSM key-value engine.
package stratadb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"

	"github.com/aryaMehta26/StrataDB/internal/memtable"
	"github.com/aryaMehta26/StrataDB/internal/storage"
)

var ErrNotFound = errors.New("stratadb: key not found")
var ErrClosed = errors.New("stratadb: database closed")

// ObjectStore must provide immutable objects and exact byte ranges.
type ObjectStore interface {
	Put(context.Context, string, []byte) error
	Range(context.Context, string, int64, int) ([]byte, error)
}

// Options defaults to fsync per mutation. NoSync sacrifices crash durability.
type Options struct {
	MemtableBytes int
	NoSync        bool
	Store         ObjectStore
	CacheBytes    int
}
type Pair struct {
	Key   string
	Value []byte
}
type Stats struct {
	UserBytes   uint64
	WALBytes    uint64
	TableBytes  uint64
	BloomSkips  uint64
	BlockReads  uint64
	CacheHits   uint64
	RemoteReads uint64
	Flushes     uint64
	Compactions uint64
}
type cacheEntry struct {
	key  string
	data []byte
}
type DB struct {
	mu        sync.Mutex
	dir       string
	opts      Options
	wal       *os.File
	lock      *os.File
	mem       memtable.Table
	tables    []storage.Table
	closed    bool
	failed    error
	stats     Stats
	cache     []cacheEntry
	cacheSize int
}

func Open(dir string, opts Options) (*DB, error) {
	if opts.MemtableBytes <= 0 {
		opts.MemtableBytes = 4 << 20
	}
	if opts.CacheBytes < 0 {
		return nil, errors.New("negative cache size")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "LOCK"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("database already open: %w", err)
	}
	d := &DB{dir: dir, opts: opts, lock: lock}
	fail := func(err error) (*DB, error) {
		if d.wal != nil {
			d.wal.Close()
		}
		lock.Close()
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "MANIFEST"))
	if err == nil {
		if d.tables, err = storage.LoadManifest(b); err != nil {
			return fail(err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	for _, t := range d.tables {
		if len(t.Blocks) == 0 || len(t.Bloom) == 0 {
			return fail(storage.ErrCorrupt)
		}
		if t.Remote && opts.Store == nil {
			return fail(errors.New("remote tables require an object store"))
		}
		if !t.Remote {
			s, e := os.Stat(d.path(t))
			if e != nil {
				return fail(e)
			}
			if s.Size() != t.Bytes {
				return fail(storage.ErrCorrupt)
			}
		}
	}
	d.wal, err = os.OpenFile(filepath.Join(dir, "WAL"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fail(err)
	}
	var offset int64
	for {
		e, n, err := storage.Read(d.wal)
		if err == io.EOF {
			break
		}
		if err == io.ErrUnexpectedEOF {
			if err = d.wal.Truncate(offset); err != nil {
				return fail(err)
			}
			break
		}
		if err != nil {
			return fail(err)
		}
		offset += int64(n)
		d.mem.Put(e)
	}
	if _, err = d.wal.Seek(0, io.SeekEnd); err != nil {
		return fail(err)
	}
	if err = d.wal.Sync(); err != nil {
		return fail(err)
	}
	if err = storage.SyncDir(dir); err != nil {
		return fail(err)
	}
	return d, nil
}
func (d *DB) path(t storage.Table) string { return filepath.Join(d.dir, t.ID+".sst") }
func (d *DB) ready() error {
	if d.closed {
		return ErrClosed
	}
	return d.failed
}
func (d *DB) Put(key string, value []byte) error {
	return d.write(memtable.Entry{Key: key, Value: append([]byte(nil), value...)})
}
func (d *DB) Delete(key string) error { return d.write(memtable.Entry{Key: key, Deleted: true}) }
func (d *DB) write(e memtable.Entry) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ready(); err != nil {
		return err
	}
	p, err := storage.Encode(e)
	if err != nil {
		return err
	}
	n, err := d.wal.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err == nil && !d.opts.NoSync {
		err = d.wal.Sync()
	}
	if err != nil {
		d.failed = err
		return err
	}
	d.stats.WALBytes += uint64(n)
	d.stats.UserBytes += uint64(len(e.Key) + len(e.Value))
	d.mem.Put(e)
	if d.mem.Size >= d.opts.MemtableBytes {
		if err = d.flush(); err != nil {
			d.failed = err
			return err
		}
	}
	return nil
}
func (d *DB) block(ctx context.Context, t storage.Table, b storage.Block) ([]byte, error) {
	d.stats.BlockReads++
	if !t.Remote {
		return storage.LocalBlock(d.path(t), b)
	}
	key := fmt.Sprintf("%s:%d", t.ID, b.Offset)
	for i, c := range d.cache {
		if c.key == key {
			d.stats.CacheHits++
			d.cache = append(append(d.cache[:i:i], d.cache[i+1:]...), c)
			return c.data, nil
		}
	}
	d.stats.RemoteReads++
	p, err := d.opts.Store.Range(ctx, t.ID+".sst", b.Offset, b.Length)
	if err != nil {
		return nil, err
	}
	if len(p) != b.Length {
		return nil, io.ErrUnexpectedEOF
	}
	if _, err = storage.DecodeBlock(p); err != nil {
		return nil, err
	}
	if len(p) <= d.opts.CacheBytes {
		for d.cacheSize+len(p) > d.opts.CacheBytes && len(d.cache) > 0 {
			d.cacheSize -= len(d.cache[0].data)
			d.cache = d.cache[1:]
		}
		d.cache = append(d.cache, cacheEntry{key, p})
		d.cacheSize += len(p)
	}
	return p, nil
}
func (d *DB) Get(key string) ([]byte, error) { return d.GetContext(context.Background(), key) }
func (d *DB) GetContext(ctx context.Context, key string) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ready(); err != nil {
		return nil, err
	}
	if e, ok := d.mem.Get(key); ok {
		if e.Deleted {
			return nil, ErrNotFound
		}
		return append([]byte(nil), e.Value...), nil
	}
	for i := len(d.tables) - 1; i >= 0; i-- {
		t := d.tables[i]
		if !t.MayContain(key) {
			d.stats.BloomSkips++
			continue
		}
		p, err := d.block(ctx, t, t.BlockFor(key))
		if err != nil {
			return nil, err
		}
		entries, err := storage.DecodeBlock(p)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.Key == key {
				if e.Deleted {
					return nil, ErrNotFound
				}
				return e.Value, nil
			}
		}
	}
	return nil, ErrNotFound
}

// Scan returns a sorted snapshot in [start,end). Empty end is unbounded.
func (d *DB) Scan(start, end string) ([]Pair, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ready(); err != nil {
		return nil, err
	}
	entries, err := d.merge(context.Background())
	if err != nil {
		return nil, err
	}
	out := []Pair{}
	for _, e := range entries {
		if !e.Deleted && e.Key >= start && (end == "" || e.Key < end) {
			out = append(out, Pair{e.Key, append([]byte(nil), e.Value...)})
		}
	}
	return out, nil
}
func (d *DB) merge(ctx context.Context) ([]memtable.Entry, error) {
	m := map[string]memtable.Entry{}
	for _, t := range d.tables {
		for _, b := range t.Blocks {
			p, err := d.block(ctx, t, b)
			if err != nil {
				return nil, err
			}
			es, err := storage.DecodeBlock(p)
			if err != nil {
				return nil, err
			}
			for _, e := range es {
				m[e.Key] = e
			}
		}
	}
	for _, e := range d.mem.Entries() {
		m[e.Key] = e
	}
	out := make([]memtable.Entry, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
func (d *DB) publish(entries []memtable.Entry, level int, remote bool) (storage.Table, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return storage.Table{}, err
	}
	t, p, err := storage.Build(hex.EncodeToString(id[:]), level, entries)
	if err != nil {
		return t, err
	}
	t.Remote = remote
	if remote {
		err = d.opts.Store.Put(context.Background(), t.ID+".sst", p)
	} else {
		err = storage.Atomic(d.path(t), p)
	}
	if err == nil {
		d.stats.TableBytes += uint64(len(p))
	}
	return t, err
}
func (d *DB) checkpoint(tables []storage.Table) error {
	if err := storage.SaveManifest(filepath.Join(d.dir, "MANIFEST"), tables); err != nil {
		return err
	}
	d.tables = tables
	if err := d.wal.Truncate(0); err != nil {
		return err
	}
	if _, err := d.wal.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := d.wal.Sync(); err != nil {
		return err
	}
	d.mem = memtable.Table{}
	return nil
}
func (d *DB) flush() error {
	if d.mem.Count == 0 {
		return nil
	}
	t, err := d.publish(d.mem.Entries(), 0, false)
	if err != nil {
		return err
	}
	next := append(append([]storage.Table(nil), d.tables...), t)
	if err = d.checkpoint(next); err == nil {
		d.stats.Flushes++
	}
	return err
}
func (d *DB) Flush() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ready(); err != nil {
		return err
	}
	err := d.flush()
	if err != nil {
		d.failed = err
	}
	return err
}

// Compact merges all tables and the memtable into one L1 table. Tombstones
// remain to make replay safe if a crash precedes WAL retirement. It blocks writes.
// remote uploads the output before publishing the manifest; upload failure
// leaves the existing database readable. Object deletion is intentionally manual.
func (d *DB) Compact(remote bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ready(); err != nil {
		return err
	}
	if remote && d.opts.Store == nil {
		return errors.New("no object store configured")
	}
	entries, err := d.merge(context.Background())
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	t, err := d.publish(entries, 1, remote)
	if err != nil {
		return err
	}
	old := d.tables
	if err = d.checkpoint([]storage.Table{t}); err != nil {
		d.failed = err
		return err
	}
	d.stats.Compactions++
	for _, o := range old {
		if !o.Remote {
			_ = os.Remove(d.path(o))
		}
	}
	return nil
}
func (d *DB) Stats() Stats { d.mu.Lock(); defer d.mu.Unlock(); return d.stats }
func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	err := d.wal.Sync()
	ce := d.wal.Close()
	le := d.lock.Close()
	return errors.Join(err, ce, le, d.failed)
}
