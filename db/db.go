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

func (db *DB) create(stmt string, args ...interface{}) error {
	if _, err := db.db.Exec(stmt, args...); err != nil {
		if sqliteErr, ok := err.(sqlite.Error); ok && sqliteErr.Code == sqlite.ErrConstraint {
			return ErrConflict
		}
		return err
	}
	return nil
}

func (db *DB) save(stmt string, args ...interface{}) error {
	res, err := db.db.Exec(stmt, args...)
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

func (db *DB) delete(stmt string, args ...interface{}) error {
	_, err := db.db.Exec(stmt, args...)
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
	return r.db.create(q, r.Name, r.Description)
}

func (r *Realm) Save() error {
	q := `UPDATE realms SET description = $1 WHERE name = $2`
	return r.db.save(q, r.Description, r.Name)
}

func (r *Realm) Delete() error {
	q := `DELETE FROM realms WHERE name = $1`
	return r.db.delete(q, r.Name)
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

func (r *Realm) GetAllPrefixes() ([]*Prefix, error) {
	q := `SELECT prefix, prefixes.description FROM prefixes INNER JOIN realms USING (realm_id) WHERE realms.name = $1 ORDER BY prefixBytes(prefix)`
	rows, err := r.db.db.Query(q, r.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ret []*Prefix
	for rows.Next() {
		var ipnet, desc string
		if err = rows.Scan(&ipnet, &desc); err != nil {
			return nil, err
		}
		_, n, err := net.ParseCIDR(ipnet)
		if err != nil {
			return nil, err
		}
		ret = append(ret, &Prefix{
			db:          r.db,
			realm:       r.Name,
			Prefix:      n,
			Description: desc,
		})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return ret, nil
}

// Prefixes

type Prefix struct {
	db          *DB
	realm       string
	Prefix      *net.IPNet
	Description string
}

func (p *Prefix) Create() error {
	q := `INSERT INTO prefixes VALUES (NULL, (SELECT realm_id FROM realms WHERE name = $1), $2, $3)`
	return p.db.create(q, p.realm, p.Prefix.String(), p.Description)
}

func (p *Prefix) Save() error {
	q := `UPDATE prefixes SET description = $1 WHERE prefix = $2 AND realm_id = (SELECT realm_id FROM realms WHERE name = $3)`
	return p.db.save(q, p.Description, p.Prefix.String(), p.realm)
}

func (p *Prefix) Delete() error {
	q := `DELETE FROM prefixes WHERE prefix = $1 AND realm_id = (SELECT realm_id FROM realms WHERE name = $2)`
	return p.db.delete(q, p.Prefix.String(), p.realm)
}

func (p *Prefix) Get() error {
	q := `SELECT prefixes.description FROM prefixes INNER JOIN realms USING (realm_id) WHERE prefix = $1 AND realms.name = $2`
	if err := p.db.db.QueryRow(q, p.Prefix.String(), p.realm).Scan(&p.Description); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (p *Prefix) GetLongestMatch() (*Prefix, error) {
	matches, err := p.GetMatches()
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, ErrNotFound
	}
	return matches[0], nil
}

func (p *Prefix) GetMatches() (matches []*Prefix, err error) {
	q := `SELECT prefix, prefixes.description FROM prefixes INNER JOIN realms USING (realm_id) WHERE isSubnetOf(prefix, $1) ORDER BY prefixLen(prefix) DESC`
	rows, err := p.db.db.Query(q, p.Prefix.String())
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
