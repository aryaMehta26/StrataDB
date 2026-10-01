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

func TestS3RejectsBrokenRangeResponses(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	cases := []struct{ name, contentRange, body string }{
		{"ignored range", "", "abcdefgh"},
		{"wrong offset", "bytes 0-3/8", "abcd"},
		{"short payload", "bytes 2-5/8", "cd"},
		{"oversized payload", "bytes 2-5/8", "cdefg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.contentRange != "" {
					w.Header().Set("Content-Range", tc.contentRange)
				}
				w.WriteHeader(http.StatusPartialContent)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			store, err := NewS3(context.Background(), "bucket", "", server.URL)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Range(context.Background(), "table.sst", 2, 4); err == nil {
				t.Fatal("accepted an invalid ranged response")
			}
		})
	}
}

func TestS3RejectsInvalidRangesBeforeNetwork(t *testing.T) {
	// A nil client makes an accidental network attempt fail this test immediately.
	store := &S3{}
	for _, tc := range []struct {
		offset int64
		length int
	}{{-1, 4}, {0, 0}, {0, -1}} {
		if _, err := store.Range(context.Background(), "key", tc.offset, tc.length); err == nil {
			t.Fatal("accepted invalid range", tc)
		}
	}
}
