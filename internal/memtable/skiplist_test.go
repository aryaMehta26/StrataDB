package memtable

import (
	"fmt"
	"testing"
)

func TestOrderingAndReplacement(t *testing.T) {
	var m Table
	for i := 999; i >= 0; i-- {
		m.Put(Entry{Key: fmt.Sprintf("%04d", i), Value: []byte("v")})
	}
	before := m.Size
	m.Put(Entry{Key: "0001", Deleted: true})
	if m.Count != 1000 || m.Size != before-1 {
		t.Fatal("incorrect replacement accounting")
	}
	entries := m.Entries()
	for i, e := range entries {
		if e.Key != fmt.Sprintf("%04d", i) {
			t.Fatal("incorrect ordering")
		}
		got, ok := m.Get(e.Key)
		if !ok || got.Key != e.Key {
			t.Fatal("lookup failed")
		}
	}
	if e, ok := m.Get("0001"); !ok || !e.Deleted {
		t.Fatal("lost tombstone")
	}
	if _, ok := m.Get("missing"); ok {
		t.Fatal("false hit")
	}
}
