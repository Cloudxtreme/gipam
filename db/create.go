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

  prefixlen INTEGER,
  upper64 INTEGER,
  lower64 INTEGER,
  upper64_max INTEGER,
  lower64_max INTEGER,

  UNIQUE (realm_id, prefix)
)`,

	`
CREATE TRIGGER IF NOT EXISTS prefixes_insert_denormalized
  AFTER INSERT ON prefixes
  FOR EACH ROW
  BEGIN
    UPDATE prefixes
      SET prefixlen = prefixLen(NEW.prefix),
          upper64 = prefixAsInt(NEW.prefix, 1, 0),
          lower64 = prefixAsInt(NEW.prefix, 0, 0),
          upper64_max = prefixAsInt(NEW.prefix, 1, 1),
          lower64_max = prefixAsInt(NEW.prefix, 0, 1)
      WHERE rowid = NEW.rowid;
  END
`,
	`
CREATE TRIGGER IF NOT EXISTS prefixes_update_denormalized
  AFTER UPDATE OF prefix ON prefixes
  FOR EACH ROW
  BEGIN
    UPDATE prefixes
      SET prefixlen = prefixLen(NEW.prefix),
          upper64 = prefixAsInt(NEW.prefix, 1, 0),
          lower64 = prefixAsInt(NEW.prefix, 0, 0),
          upper64_max = prefixAsInt(NEW.prefix, 1, 1),
          lower64_max = prefixAsInt(NEW.prefix, 0, 1)
      WHERE rowid = NEW.rowid;
  END
`,

	// 	`CREATE TABLE IF NOT EXISTS hosts (
	//   id INTEGER PRIMARY KEY,
	//   realm_id INTEGER REFERENCES realms(id) ON DELETE CASCADE ON UPDATE CASCADE,
	//   hostname TEXT NOT NULL,
	//   description TEXT
	// )`,
	// 	`CREATE TABLE IF NOT EXISTS host_addrs (
	//   id INTEGER PRIMARY KEY,
	//   host_id INTEGER REFERENCES hosts(id) ON DELETE CASCADE ON UPDATE CASCADE,
	//   address BLOB NOT NULL
	// )`,
	// 	`CREATE TABLE IF NOT EXISTS domains (
	//   id INTEGER PRIMARY KEY,
	//   realm_id INTEGER REFERENCES realms(id) ON DELETE CASCADE ON UPDATE CASCADE,
	//   name TEXT NOT NULL,
	//   primary_ns TEXT NOT NULL,
	//   email TEXT NOT NULL,
	//   slave_refresh INTEGER DEFAULT 900,
	//   slave_retry INTEGER DEFAULT 900,
	//   slave_expiry INTEGER DEFAULT 1814400,
	//   nxdomain_ttl INTEGER DEFAULT 600,
	//   serial INTEGER DEFAULT 0,
	// )`,
	// 	`CREATE TABLE IF NOT EXISTS domain_records (
	//   id INTEGER PRIMARY KEY,
	//   domain_id INTEGER REFERENCES domains(id) ON DELETE CASCADE ON UPDATE CASCADE,
	//   record TEXT NOT NULL,
	// )`,
}
