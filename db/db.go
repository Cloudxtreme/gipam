package db

import (
	"database/sql"
	"errors"

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
