package stratadb_test

import (
	"fmt"
	"os"

	strata "github.com/aryaMehta26/StrataDB"
)

func ExampleDB_Scan() {
	dir, err := os.MkdirTemp("", "stratadb-example-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	db, err := strata.Open(dir, strata.Options{})
	if err != nil {
		panic(err)
	}
	defer db.Close()
	for _, key := range []string{"user:003", "user:001", "user:002"} {
		if err := db.Put(key, []byte("active")); err != nil {
			panic(err)
		}
	}
	if err := db.Delete("user:002"); err != nil {
		panic(err)
	}
	rows, err := db.Scan("user:", "user;")
	if err != nil {
		panic(err)
	}
	for _, row := range rows {
		fmt.Printf("%s: %s\n", row.Key, row.Value)
	}
	// Output:
	// user:001: active
	// user:003: active
}
