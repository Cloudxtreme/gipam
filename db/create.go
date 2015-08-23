package db

// All create statements are grouped into 3 blocks: normalized fields,
// denormalized fields, and table constraints.

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
  parent_id INTEGER REFERENCES prefixes(prefix_id) ON DELETE RESTRICT ON UPDATE CASCADE,
  prefix TEXT UNIQUE NOT NULL,
  description TEXT,
  UNIQUE (realm_id, prefix)
)`,

	`
CREATE TABLE IF NOT EXISTS hosts (
  host_id INTEGER PRIMARY KEY,
  realm_id INTEGER REFERENCES realms(realm_id) ON DELETE CASCADE ON UPDATE CASCADE,
  hostname TEXT NOT NULL,
  description TEXT,
  UNIQUE (realm_id, hostname)
)`,

	`
CREATE TABLE IF NOT EXISTS host_addrs (
  addr_id INTEGER PRIMARY KEY,
  realm_id INTEGER REFERENCES realms(realm_id) ON DELETE CASCADE ON UPDATE CASCADE,
  host_id INTEGER REFERENCES hosts(id) ON DELETE CASCADE ON UPDATE CASCADE,
  address TEXT NOT NULL,
  UNIQUE (realm_id, address)
)`,

	`
CREATE TABLE IF NOT EXISTS domains (
  domain_id INTEGER PRIMARY KEY,
  realm_id INTEGER REFERENCES realms(realm_id) ON DELETE CASCADE ON UPDATE CASCADE,
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
  domain_id INTEGER REFERENCES domains(domain_id) ON DELETE CASCADE ON UPDATE CASCADE,
  record TEXT NOT NULL,
  UNIQUE (domain_id, record)
)`,
}
