package storage

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/aryaMehta26/StrataDB/internal/memtable"
	"io"
	"testing"
)

func TestFrames(t *testing.T) {
	p, e := Encode(memtable.Entry{Key: "a", Value: []byte("value")})
	if e != nil {
		t.Fatal(e)
	}
	for n := 1; n < len(p); n++ {
		_, _, e := Read(bytes.NewReader(p[:n]))
		if !errors.Is(e, io.ErrUnexpectedEOF) {
			t.Fatalf("cut %d: %v", n, e)
		}
	}
	p[len(p)-1] ^= 1
	if _, _, e := Read(bytes.NewReader(p)); !errors.Is(e, ErrCorrupt) {
		t.Fatal(e)
	}
}
func TestBloomAndBlocks(t *testing.T) {
	var es []memtable.Entry
	for i := 0; i < 10000; i++ {
		es = append(es, memtable.Entry{Key: fmt.Sprintf("k%06d", i*2), Value: []byte("v")})
	}
	table, p, e := Build("test", 0, es)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range es {
		if !table.MayContain(entry.Key) {
			t.Fatal("false negative")
		}
		b := table.BlockFor(entry.Key)
		out, e := DecodeBlock(p[b.Offset : int(b.Offset)+b.Length])
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, v := range out {
			if v.Key == entry.Key {
				found = true
			}
		}
		if !found {
			t.Fatal("bad sparse index")
		}
	}
	fp := 0
	for i := 0; i < 9999; i++ {
		if table.MayContain(fmt.Sprintf("k%06d", i*2+1)) {
			fp++
		}
	}
	t.Logf("bloom false positives: %d/9999 (%.3f%%)", fp, float64(fp)*100/9999)
	if fp > 400 {
		t.Fatal("false positive rate exceeds 4%")
	}
}
func FuzzRead(f *testing.F) {
	p, _ := Encode(memtable.Entry{Key: "seed"})
	f.Add(p)
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, p []byte) {
		if len(p) > 1<<20 {
			t.Skip()
		}
		_, _, _ = Read(bytes.NewReader(p))
	})
}
