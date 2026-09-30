package tiering

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestS3WireRange(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	var mu sync.Mutex
	var object []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != "/bucket/prefix/table.sst" {
			t.Errorf("path %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		switch r.Method {
		case "PUT":
			var e error
			object, e = io.ReadAll(r.Body)
			if e != nil {
				t.Error(e)
			}
			w.Header().Set("ETag", "\"test\"")
		case "GET":
			if r.Header.Get("Range") != "bytes=2-5" {
				t.Error("missing byte range")
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 2-5/%d", len(object)))
			w.Header().Set("Content-Length", "4")
			w.WriteHeader(http.StatusPartialContent)
			w.Write(object[2:6])
		}
	}))
	defer server.Close()
	s, e := NewS3(context.Background(), "bucket", "prefix", server.URL)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Put(context.Background(), "table.sst", []byte("abcdefgh")); e != nil {
		t.Fatal(e)
	}
	b, e := s.Range(context.Background(), "table.sst", 2, 4)
	if e != nil || string(b) != "cdef" {
		t.Fatalf("%q %v", b, e)
	}
}
