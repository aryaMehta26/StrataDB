package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/aryaMehta26/StrataDB/internal/memtable"
	"hash/crc32"
	"hash/fnv"
	"io"
	"os"
	"sort"
)

type Block struct {
	First  string
	Offset int64
	Length int
}
type Table struct {
	ID     string
	Level  int
	Remote bool
	Blocks []Block
	Bloom  []byte
	Min    string
	Max    string
	Bytes  int64
	Count  int
}

func hashes(k string) (uint64, uint64) {
	h := fnv.New64a()
	h.Write([]byte(k))
	a := h.Sum64()
	return a, (a>>17 | a<<47) | 1
}
func (t Table) MayContain(k string) bool {
	if k < t.Min || k > t.Max {
		return false
	}
	a, b := hashes(k)
	for i := uint64(0); i < 7; i++ {
		p := (a + i*b) % uint64(len(t.Bloom)*8)
		if t.Bloom[p/8]&(1<<(p%8)) == 0 {
			return false
		}
	}
	return true
}
func Build(id string, level int, entries []memtable.Entry) (Table, []byte, error) {
	t := Table{ID: id, Level: level, Count: len(entries), Bloom: make([]byte, (len(entries)*10+7)/8)}
	if len(entries) == 0 {
		return t, nil, fmt.Errorf("empty table")
	}
	t.Min = entries[0].Key
	t.Max = entries[len(entries)-1].Key
	var data bytes.Buffer
	blockSize := 0
	for _, e := range entries {
		p, err := Encode(e)
		if err != nil {
			return t, nil, err
		}
		if blockSize == 0 {
			t.Blocks = append(t.Blocks, Block{First: e.Key, Offset: int64(data.Len())})
		}
		data.Write(p)
		blockSize += len(p)
		t.Blocks[len(t.Blocks)-1].Length += len(p)
		if blockSize >= 4096 {
			blockSize = 0
		}
		a, b := hashes(e.Key)
		for i := uint64(0); i < 7; i++ {
			pos := (a + i*b) % uint64(len(t.Bloom)*8)
			t.Bloom[pos/8] |= 1 << (pos % 8)
		}
	}
	t.Bytes = int64(data.Len())
	return t, data.Bytes(), nil
}
func (t Table) BlockFor(k string) Block {
	i := sort.Search(len(t.Blocks), func(i int) bool { return t.Blocks[i].First > k }) - 1
	if i < 0 {
		i = 0
	}
	return t.Blocks[i]
}
func DecodeBlock(b []byte) ([]memtable.Entry, error) {
	r := bytes.NewReader(b)
	var out []memtable.Entry
	for r.Len() > 0 {
		e, _, err := Read(r)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
func LocalBlock(path string, b Block) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	p := make([]byte, b.Length)
	_, err = f.ReadAt(p, b.Offset)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return p, err
}

// The checksum protects indexes and Bloom bits as well as table membership.
type manifest struct {
	Version int
	CRC     uint32
	Tables  json.RawMessage
}

func SaveManifest(path string, t []Table) error {
	payload, err := json.Marshal(t)
	if err != nil {
		return err
	}
	b, err := json.Marshal(manifest{Version: 1, CRC: crc32.ChecksumIEEE(payload), Tables: payload})
	if err != nil {
		return err
	}
	return Atomic(path, b)
}
func LoadManifest(b []byte) ([]Table, error) {
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%w: manifest JSON", ErrCorrupt)
	}
	if m.Version != 1 || crc32.ChecksumIEEE(m.Tables) != m.CRC {
		return nil, fmt.Errorf("%w: manifest checksum/version", ErrCorrupt)
	}
	var tables []Table
	if err := json.Unmarshal(m.Tables, &tables); err != nil {
		return nil, ErrCorrupt
	}
	seen := map[string]bool{}
	for _, t := range tables {
		if len(t.ID) != 32 || seen[t.ID] || len(t.Blocks) == 0 || len(t.Bloom) == 0 || t.Count <= 0 || t.Min > t.Max {
			return nil, ErrCorrupt
		}
		for _, c := range t.ID {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return nil, ErrCorrupt
			}
		}
		seen[t.ID] = true
		var off int64
		for i, b := range t.Blocks {
			if b.Offset != off || b.Length <= 0 || b.Length > MaxRecord+4104 || i == 0 && b.First != t.Min || i > 0 && b.First <= t.Blocks[i-1].First {
				return nil, ErrCorrupt
			}
			off += int64(b.Length)
		}
		if off != t.Bytes {
			return nil, ErrCorrupt
		}
	}
	return tables, nil
}
