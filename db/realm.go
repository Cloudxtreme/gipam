package db

import (
	"database/sql"
	"net"
)

type Realm struct {
	db   *sql.DB
	Id   int64
	Name string
}

func (db *DB) Realm(id int64) (*Realm, error) {
	q := `SELECT name FROM realms WHERE realm_id = $1`
	var name string
	if err := db.db.QueryRow(q, id).Scan(&name); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &Realm{
		db:   db.db,
		Id:   id,
		Name: name,
	}, nil
}

func (db *DB) Realms() ([]*Realm, error) {
	q := `SELECT realm_id, name, description FROM realms ORDER BY name`
	rows, err := db.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ret := []*Realm{}
	for rows.Next() {
		var id int64
		var name string
		if err = rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		ret = append(ret, &Realm{
			db:   db.db,
			Id:   id,
			Name: name,
		})
	}

	return ret, nil
}

func (db *DB) CreateRealm(name string) (*Realm, error) {
	q := `INSERT INTO realms (name) VALUES ($1)`
	res, err := db.db.Exec(q, name)
	if err != nil {
		if errIsAlreadyExists(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	last, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Realm{
		db:   db,
		Id:   last,
		Name: name,
	}, nil
}

func (r *Realm) Domain(name string) *Domain {
	return &Domain{
		db:    r.db,
		realm: r.Name,
		Name:  name,
	}
}

func (r *Realm) Host(hostname string) *Host {
	return &Host{
		db:       r.db,
		realm:    r.Name,
		Hostname: hostname,
	}
}

func (r *Realm) Save() error {
	q := `UPDATE realms SET name = $1 WHERE realm_id = $2`
	res, err := r.db.Exec(q, r.Name, r.Id)
	if err != nil {
		return err
	}
	return mustHaveChanged(res)
}

func (r *Realm) Delete() error {
	q := `DELETE FROM realms WHERE realm_id = $1`
	if _, err := r.db.Exec(q, r.Id); err != nil {
		return err
	}
	return nil
}

type PrefixTree struct {
	*Prefix
	Children []*PrefixTree
}

func (r *Realm) GetPrefixTree() (roots []*PrefixTree, err error) {
	q := `
SELECT prefix_id, parent_id, prefix, prefixes.description
FROM prefixes INNER JOIN realms USING (realm_id)
WHERE realms.name = $1
ORDER BY parent_id
`
	rows, err := r.db.Query(q, r.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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
