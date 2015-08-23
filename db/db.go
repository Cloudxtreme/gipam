package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	sqlite "github.com/mattn/go-sqlite3"
)

var ErrNotFound = errors.New("Object not found in DB")
var ErrAlreadyExists = errors.New("Object already exists in DB")

type DB struct {
	db *sql.DB
}

func New(path string) (*DB, error) {
	db, err := sql.Open("sqlite3_gipam", path)
	if err != nil {
		return nil, err
	}

	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	for _, stmt := range createStmts {
		if _, err = db.Exec(stmt); err != nil {
			db.Close()
			return nil, err
		}
	}

	return &DB{db}, nil
}

func (db *DB) Realm(name string) *Realm {
	return &Realm{
		db:   db.db,
		Name: name,
	}
}

func (db *DB) Close() error {
	return db.db.Close()
}

func errIsAlreadyExists(err error) bool {
	if sqliteErr, ok := err.(sqlite.Error); ok && (sqliteErr.ExtendedCode == sqlite.ErrConstraintUnique || sqliteErr.ExtendedCode == sqlite.ErrConstraintPrimaryKey) {
		return true
	}
	return false
}

func mustHaveChanged(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func printExplain(db *sql.Tx, stmt string, args ...interface{}) {
	rows, err := db.Query("EXPLAIN "+stmt, args...)
	if err != nil {
		fmt.Printf("Error getting query plan: %s\n", err)
		return
	}
	defer rows.Close()
	w := new(tabwriter.Writer)
	w.Init(os.Stdout, 5, 0, 1, ' ', tabwriter.Debug)
	fmt.Fprintf(w, "addr\topcode\tp1\tp2\tp3\tp4\tp5\tcomment\n")
	for rows.Next() {
		var a, b, c, d, e, f, g, h sql.NullString
		if err = rows.Scan(&a, &b, &c, &d, &e, &f, &g, &h); err != nil {
			fmt.Printf("Row decode error: %s", err)
			return
		}
		fmt.Fprintf(w, "%s \t%s \t%s \t%s \t%s \t%s \t%s \t%s \n", a.String, b.String, c.String, d.String, e.String, f.String, g.String, h.String)
	}
	w.Flush()
	if err = rows.Err(); err != nil {
		fmt.Printf("Query error: %s\n", err)
	}
}
