package main

import "database/sql"

var createStmts = []string{
	`
CREATE TABLE IF NOT EXISTS realms (
  realm_id INTEGER PRIMARY KEY,
  name TEXT UNIQUE NOT NULL,
  description TEXT
)`,

	`
CREATE TABLE IF NOT EXISTS prefixes (
  prefix_id INTEGER PRIMARY KEY,
  realm_id INTEGER NOT NULL REFERENCES realms ON DELETE CASCADE ON UPDATE CASCADE,
  parent_id INTEGER REFERENCES prefixes ON DELETE CASCADE ON UPDATE CASCADE,
  prefix TEXT NOT NULL,
  description TEXT,
  UNIQUE (realm_id, prefix)
)`,

	`
CREATE TABLE IF NOT EXISTS hosts (
  host_id INTEGER PRIMARY KEY,
  realm_id INTEGER REFERENCES realms ON DELETE CASCADE ON UPDATE CASCADE,
  hostname TEXT NOT NULL,
  description TEXT,
  UNIQUE (realm_id, hostname)
)`,

	`
CREATE TABLE IF NOT EXISTS host_addrs (
  addr_id INTEGER PRIMARY KEY,
  realm_id INTEGER REFERENCES realms ON DELETE CASCADE ON UPDATE CASCADE,
  host_id INTEGER REFERENCES hosts ON DELETE CASCADE ON UPDATE CASCADE,
  address TEXT NOT NULL,
  description TEXT,
  UNIQUE (realm_id, address)
)`,

	`
CREATE TABLE IF NOT EXISTS domains (
  domain_id INTEGER PRIMARY KEY,
  realm_id INTEGER REFERENCES realms ON DELETE CASCADE ON UPDATE CASCADE,
  name TEXT NOT NULL,
  primary_ns TEXT NOT NULL,
  email TEXT NOT NULL,
  slave_refresh INTEGER NOT NULL,
  slave_retry INTEGER NOT NULL,
  slave_expiry INTEGER NOT NULL,
  nxdomain_ttl INTEGER NOT NULL,
  serial TEXT NOT NULL,
  UNIQUE (realm_id, name)
)`,

	`
CREATE TABLE IF NOT EXISTS domain_records (
  record_id INTEGER PRIMARY KEY,
  domain_id INTEGER REFERENCES domains ON DELETE CASCADE ON UPDATE CASCADE,
  record TEXT NOT NULL,
  UNIQUE (domain_id, record)
)`,
	`
PRAGMA foreign_keys = ON`,
}

func NewDB(path string) (*sql.DB, error) {
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

	return db, nil
}
