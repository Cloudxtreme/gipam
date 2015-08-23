package db

import (
	"database/sql"
	"errors"
	"net"

	sqlite "github.com/mattn/go-sqlite3"
)

var ErrNotFound = errors.New("Object not found in DB")
var ErrConflict = errors.New("Object already exists in DB")

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

func (db *DB) create(tx *sql.Tx, stmt string, args ...interface{}) error {
	ex := db.db.Exec
	if tx != nil {
		ex = tx.Exec
	}
	if _, err := ex(stmt, args...); err != nil {
		if sqliteErr, ok := err.(sqlite.Error); ok && sqliteErr.Code == sqlite.ErrConstraint {
			return ErrConflict
		}
		return err
	}
	return nil
}

func (db *DB) save(tx *sql.Tx, stmt string, args ...interface{}) error {
	ex := db.db.Exec
	if tx != nil {
		ex = tx.Exec
	}
	res, err := ex(stmt, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) delete(tx *sql.Tx, stmt string, args ...interface{}) error {
	ex := db.db.Exec
	if tx != nil {
		ex = tx.Exec
	}
	_, err := ex(stmt, args...)
	return err
}

// Realms

type Realm struct {
	db          *DB
	Name        string
	Description string
}

func (db *DB) Realm(name string) *Realm {
	return &Realm{
		db:   db,
		Name: name,
	}
}

func (r *Realm) Create() error {
	q := `INSERT INTO realms VALUES (NULL, $1, $2)`
	return r.db.create(nil, q, r.Name, r.Description)
}

func (r *Realm) Save() error {
	q := `UPDATE realms SET description = $1 WHERE name = $2`
	return r.db.save(nil, q, r.Description, r.Name)
}

func (r *Realm) Delete() error {
	q := `DELETE FROM realms WHERE name = $1`
	return r.db.delete(nil, q, r.Name)
}

func (r *Realm) Get() error {
	q := `SELECT description FROM realms WHERE name = $1`
	if err := r.db.db.QueryRow(q, r.Name).Scan(&r.Description); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (r *Realm) Prefix(prefix *net.IPNet) *Prefix {
	return &Prefix{
		db:     r.db,
		realm:  r.Name,
		Prefix: prefix,
	}
}

func (r *Realm) GetPrefixTree() (roots []*PrefixTree, err error) {
	q := `
SELECT prefix_id, parent_id, prefix, prefixes.description
FROM prefixes INNER JOIN realms USING (realm_id)
WHERE realms.name = $1
ORDER BY parent_id
`
	rows, err := r.db.db.Query(q, r.Name)
	if err != nil {
		return nil, err
	}

	prefixes := map[int64]*PrefixTree{}
	for rows.Next() {
		var prefixId int64
		var parentId *int64
		var pfx, desc string

		if err = rows.Scan(&prefixId, &parentId, &pfx, &desc); err != nil {
			return nil, err
		}
		_, n, err := net.ParseCIDR(pfx)
		if err != nil {
			return nil, err
		}
		p := &PrefixTree{
			Prefix: &Prefix{
				db:          r.db,
				realm:       r.Name,
				Prefix:      n,
				Description: desc,
			},
		}

		if parentId == nil {
			roots = append(roots, p)
		} else {
			parent := prefixes[*parentId]
			parent.Children = append(parent.Children, p)
		}
		prefixes[prefixId] = p
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return roots, nil
}

// Prefixes

type Prefix struct {
	db          *DB
	realm       string
	Prefix      *net.IPNet
	Description string
}

func (p *Prefix) Create() error {
	tx, err := p.db.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var parentId *int64
	q := `SELECT prefix_id FROM prefixes INNER JOIN realms USING (realm_id) WHERE realms.name = $1 AND isSubnetOf(prefix, $2) ORDER BY prefixLen(prefix) DESC LIMIT 1`
	err = tx.QueryRow(q, p.realm, p.Prefix.String()).Scan(&parentId)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	q = `INSERT INTO prefixes VALUES (NULL, (SELECT realm_id FROM realms WHERE name = $1), $2, $3, $4)`
	if err = p.db.create(tx, q, p.realm, parentId, p.Prefix.String(), p.Description); err != nil {
		return err
	}

	var realmId, prefixId int64
	q = `SELECT realm_id, prefix_id FROM prefixes INNDER JOIN realms USING (realm_id) WHERE name = $1 AND prefix = $2`
	if err = tx.QueryRow(q, p.realm, p.Prefix.String()).Scan(&realmId, &prefixId); err != nil {
		return err
	}

	q = `UPDATE prefixes SET parent_id = $1 WHERE realm_id = $2 AND prefix != $3 AND isSubnetOf($3, prefix)`
	if _, err = tx.Exec(q, prefixId, realmId, p.Prefix.String()); err != nil {
		return err
	}

	return tx.Commit()
}

func (p *Prefix) Save() error {
	q := `UPDATE prefixes SET description = $1 WHERE prefix = $2 AND realm_id = (SELECT realm_id FROM realms WHERE name = $3)`
	return p.db.save(nil, q, p.Description, p.Prefix.String(), p.realm)
}

func (p *Prefix) Delete() error {
	tx, err := p.db.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var realmId, prefixId int64
	var parentId *int64
	q := `SELECT prefixes.realm_id, prefix_id, parent_id FROM prefixes INNER JOIN realms USING (realm_id) WHERE realms.name = $1 AND prefix = $2`
	if err = tx.QueryRow(q, p.realm, p.Prefix.String()).Scan(&realmId, &prefixId, &parentId); err != nil {
		return err
	}

	q = `UPDATE prefixes SET parent_id = $1 WHERE realm_id = $2 AND parent_id = $3`
	if _, err = tx.Exec(q, parentId, realmId, prefixId); err != nil {
		return err
	}

	q = `DELETE FROM prefixes WHERE realm_id = $1 AND prefix_id = $2`
	if err = p.db.save(tx, q, realmId, prefixId); err != nil {
		return err
	}

	return tx.Commit()
}

func (p *Prefix) Get() error {
	q := `SELECT prefixes.description FROM prefixes INNER JOIN realms USING (realm_id) WHERE name = $1 AND prefix = $2`
	if err := p.db.db.QueryRow(q, p.realm, p.Prefix.String()).Scan(&p.Description); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (p *Prefix) GetLongestMatch() (*Prefix, error) {
	// First try a straight Get(), which will be indexed and fast.
	p = &Prefix{db: p.db, realm: p.realm, Prefix: p.Prefix}
	if err := p.Get(); err == nil {
		return p, nil
	}

	// No luck, do the expensive longest match query.
	q := `SELECT prefix, prefixes.description FROM prefixes INNER JOIN realms USING (realm_id) WHERE realms.name = $1 AND isSubnetOf(prefix, $2) ORDER BY prefixLen(prefix) DESC LIMIT 1`

	var pfx string
	if err := p.db.db.QueryRow(q, p.realm, p.Prefix.String()).Scan(&pfx, &p.Description); err != nil {
		return nil, err
	}
	_, n, err := net.ParseCIDR(pfx)
	if err != nil {
		return nil, err
	}
	p.Prefix = n
	return p, nil
}

func (p *Prefix) GetMatches() (matches []*Prefix, err error) {
	p, err = p.GetLongestMatch()
	if err != nil {
		return nil, err
	}

	q := `
WITH RECURSIVE pfx(realm_id, prefix, desc, parent_id) AS (
  SELECT prefixes.realm_id, prefix, prefixes.description, parent_id
  FROM prefixes INNER JOIN realms USING (realm_id)
  WHERE realms.name = $1 AND prefix = $2
UNION ALL
  SELECT prefixes.realm_id, prefixes.prefix, prefixes.description, prefixes.parent_id
  FROM prefixes, pfx
  WHERE pfx.parent_id IS NOT NULL AND prefixes.prefix_id = pfx.parent_id
)
SELECT prefix, desc
FROM pfx
ORDER BY prefixLen(prefix) DESC
`
	rows, err := p.db.db.Query(q, p.realm, p.Prefix.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var ipnet, desc string
		if err = rows.Scan(&ipnet, &desc); err != nil {
			return nil, err
		}
		_, n, err := net.ParseCIDR(ipnet)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &Prefix{
			db:          p.db,
			realm:       p.realm,
			Prefix:      n,
			Description: desc,
		})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}

// PrefixTree

type PrefixTree struct {
	*Prefix
	Children []*PrefixTree
}
