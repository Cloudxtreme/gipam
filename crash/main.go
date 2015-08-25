package main

import (
	"database/sql"
	"fmt"
	"log"

	sqlite "github.com/mattn/go-sqlite3"
)

func stress(a, b int) bool {
	return a == b
}

func main() {
	sql.Register("sqlite3_test",
		&sqlite.SQLiteDriver{
			ConnectHook: func(conn *sqlite.SQLiteConn) error {
				if err := conn.RegisterFunc("stress", stress, true); err != nil {
					return err
				}
				return nil
			},
		})

	db, err := sql.Open("sqlite3_test", ":memory:")
	if err != nil {
		log.Fatal(err)
	}

	for i := 0; i < 10000000; i++ {
		var f interface{}
		if err = db.QueryRow("SELECT stress(1,2)").Scan(&f); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Println("Done")
}
