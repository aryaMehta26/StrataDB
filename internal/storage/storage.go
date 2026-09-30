// Package storage owns checksummed record framing and durable file publication.
package storage

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aryaMehta26/StrataDB/internal/memtable"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"
)

const MaxRecord = 64 << 20

var ErrCorrupt = errors.New("stratadb: checksum or format corruption")

func Encode(e memtable.Entry) ([]byte, error) {
	if !utf8.ValidString(e.Key) {
		return nil, errors.New("stratadb: key must be valid UTF-8")
	}
	p, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	if len(p) > MaxRecord {
		return nil, errors.New("record too large")
	}
	b := make([]byte, 8+len(p))
	binary.LittleEndian.PutUint32(b, uint32(len(p)))
	binary.LittleEndian.PutUint32(b[4:], crc32.ChecksumIEEE(p))
	copy(b[8:], p)
	return b, nil
}

// Read returns UnexpectedEOF only for a physically incomplete final frame.
func Read(r io.Reader) (memtable.Entry, int, error) {
	var e memtable.Entry
	var h [8]byte
	n, err := io.ReadFull(r, h[:])
	if err != nil {
		if n > 0 {
			err = io.ErrUnexpectedEOF
		}
		return e, n, err
	}
	size := binary.LittleEndian.Uint32(h[:])
	if size > MaxRecord {
		return e, 8, ErrCorrupt
	}
	p := make([]byte, int(size))
	n, err = io.ReadFull(r, p)
	if err != nil {
		return e, 8 + n, io.ErrUnexpectedEOF
	}
	if crc32.ChecksumIEEE(p) != binary.LittleEndian.Uint32(h[4:]) {
		return e, 8 + n, ErrCorrupt
	}
	if err = json.Unmarshal(p, &e); err != nil {
		return e, 8 + n, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	return e, 8 + n, nil
}
func SyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func Atomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".publish-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return SyncDir(filepath.Dir(path))
}
