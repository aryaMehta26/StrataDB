// Package memtable implements a sorted skip list. Callers synchronize access.
package memtable

import "math/rand/v2"

const height = 16

type Entry struct {
	Key     string
	Value   []byte
	Deleted bool
}
type node struct {
	entry Entry
	next  [height]*node
}
type Table struct {
	head  node
	Size  int
	Count int
}

func (t *Table) Put(e Entry) {
	var prev [height]*node
	n := &t.head
	for i := height - 1; i >= 0; i-- {
		for n.next[i] != nil && n.next[i].entry.Key < e.Key {
			n = n.next[i]
		}
		prev[i] = n
	}
	if n = n.next[0]; n != nil && n.entry.Key == e.Key {
		t.Size -= len(n.entry.Value)
		n.entry = e
		t.Size += len(e.Value)
		return
	}
	n = &node{entry: e}
	t.Size += len(e.Key) + len(e.Value) + 1
	t.Count++
	h := 1
	for h < height && rand.IntN(4) == 0 {
		h++
	}
	for i := 0; i < h; i++ {
		n.next[i] = prev[i].next[i]
		prev[i].next[i] = n
	}
}
func (t *Table) Get(key string) (Entry, bool) {
	n := &t.head
	for i := height - 1; i >= 0; i-- {
		for n.next[i] != nil && n.next[i].entry.Key < key {
			n = n.next[i]
		}
	}
	n = n.next[0]
	if n != nil && n.entry.Key == key {
		return n.entry, true
	}
	return Entry{}, false
}
func (t *Table) Entries() []Entry {
	out := make([]Entry, 0, t.Count)
	for n := t.head.next[0]; n != nil; n = n.next[0] {
		out = append(out, n.entry)
	}
	return out
}
