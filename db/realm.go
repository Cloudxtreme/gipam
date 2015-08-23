package db

import (
	"database/sql"
	"net"
)

type Realm struct {
	db          *sql.DB
	Name        string
	Description string
}

func (r *Realm) Prefix(prefix *net.IPNet) *Prefix {
	return &Prefix{
		db:     r.db,
		realm:  r.Name,
		Prefix: prefix,
	}
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

func (r *Realm) Create() error {
	q := `INSERT INTO realms (name, description) VALUES ($1, $2)`
	_, err := r.db.Exec(q, r.Name, r.Description)
	if err != nil && errIsAlreadyExists(err) {
		return ErrAlreadyExists
	}
	return err
}

func (r *Realm) Save() error {
	q := `UPDATE realms SET description = $1 WHERE name = $2`
	res, err := r.db.Exec(q, r.Description, r.Name)
	if err != nil {
		return err
	}
	return mustHaveChanged(res)
}

func (r *Realm) Delete() error {
	q := `DELETE FROM realms WHERE name = $1`
	if _, err := r.db.Exec(q, r.Name); err != nil {
		return err
	}
	return nil
}

func (r *Realm) Get() error {
	q := `SELECT description FROM realms WHERE name = $1`
	if err := r.db.QueryRow(q, r.Name).Scan(&r.Description); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
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
