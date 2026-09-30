// stratadb-demo exercises persistence, deletion, scans, and optional S3 tiering.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	db "github.com/aryaMehta26/StrataDB"
	"github.com/aryaMehta26/StrataDB/tiering"
	"log"
	"os"
)

func main() {
	dir := flag.String("dir", "", "database directory (default: temporary)")
	bucket := flag.String("bucket", "", "optional S3 bucket")
	endpoint := flag.String("endpoint", "", "optional S3-compatible endpoint")
	flag.Parse()
	if e := run(*dir, *bucket, *endpoint); e != nil {
		log.Fatal(e)
	}
}
func run(dir, bucket, endpoint string) error {
	if dir == "" {
		var e error
		dir, e = os.MkdirTemp("", "stratadb-demo-*")
		if e != nil {
			return e
		}
		defer os.RemoveAll(dir)
	}
	opts := db.Options{CacheBytes: 1 << 20}
	if bucket != "" {
		s, e := tiering.NewS3(context.Background(), bucket, "stratadb-demo", endpoint)
		if e != nil {
			return e
		}
		opts.Store = s
	}
	d, e := db.Open(dir, opts)
	if e != nil {
		return e
	}
	defer d.Close()
	for _, p := range []db.Pair{{Key: "user:001", Value: []byte("Ada")}, {Key: "user:002", Value: []byte("Grace")}, {Key: "user:003", Value: []byte("Linus")}} {
		if e = d.Put(p.Key, p.Value); e != nil {
			return e
		}
	}
	if e = d.Delete("user:003"); e != nil {
		return e
	}
	if e = d.Compact(bucket != ""); e != nil {
		return e
	}
	if e = d.Close(); e != nil {
		return e
	}
	d, e = db.Open(dir, opts)
	if e != nil {
		return e
	}
	defer d.Close()
	fmt.Println("Reopened database:", dir)
	for i := 0; i < 2; i++ {
		v, e := d.Get("user:001")
		if e != nil {
			return e
		}
		fmt.Printf("Get(user:001) = %s\n", v)
	}
	rows, e := d.Scan("user:", "user;")
	if e != nil {
		return e
	}
	for _, p := range rows {
		fmt.Printf("%s → %s\n", p.Key, p.Value)
	}
	b, e := json.MarshalIndent(d.Stats(), "", "  ")
	if e != nil {
		return e
	}
	fmt.Println("Read-path counters:", string(b))
	return nil
}
