package db

import (
	"database/sql"
	"net"
)

// Prefixes

type Prefix struct {
	realm       *Realm
	Id          int64
	Prefix      *net.IPNet
	Description string
}

func (r *Realm) Prefix(prefix *net.IPNet) (*Prefix, error) {
	q := `SELECT prefix_id, description FROM prefixes WHERE realm_id=$1 AND prefix=$2`
	var id int64
	var desc string
	if err := r.db.QueryRow(q, r.Id, prefix.String()).Scan(&id, &desc); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &Prefix{
		realm:       r,
		Id:          id,
		Prefix:      prefix,
		Description: desc,
	}, nil
}

func (r *Realm) CreatePrefix(prefix *net.IPNet, description string) (*Prefix, error) {
	tx, err := p.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var parentId *int64
	q := `SELECT prefix_id FROM prefixes WHERE realm_id=$1 AND prefixIsInside($2, prefix) ORDER BY prefixLen(prefix) DESC LIMIT 1`
	err = tx.QueryRow(q, p.Id, prefix.String()).Scan(&parentId)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	q = `
INSERT INTO prefixes (realm_id, parent_id, prefix, description)
VALUES ($1, $2, $3, $4)`
	res, err = tx.Exec(q, r.Id, parentId, prefix.String(), description)
	if err != nil {
		if errIsAlreadyExists(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}

	prefixId, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	q = `
UPDATE prefixes SET parent_id = $1
WHERE realm_id = $2
AND prefixIsInside(prefix, $3)
`
	if _, err = tx.Exec(q, prefixId, r.Id, prefix.String()); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &Prefix{
		realm:       r,
		Id:          prefixId,
		Prefix:      prefix,
		Description: description,
	}, nil
}

func (p *Prefix) Save() error {
	q := `
UPDATE prefixes SET description = $1 WHERE prefix = $2 AND realm_id = $3`
	res, err := p.db.Exec(q, p.Description, p.Prefix.String(), p.realm.Id)
	if err != nil {
		return err
	}
	return mustHaveChanged(res)
}

func (p *Prefix) Delete() error {
	tx, err := p.db.Begin()
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
	if _, err = tx.Exec(q, realmId, prefixId); err != nil {
		return err
	}

	return tx.Commit()
}

func (p *Prefix) Get() error {
	q := `SELECT prefixes.description FROM prefixes INNER JOIN realms USING (realm_id) WHERE name = $1 AND prefix = $2`
	if err := p.db.QueryRow(q, p.realm, p.Prefix.String()).Scan(&p.Description); err != nil {
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

	// No luck, do the more expensive longest match query.
	q := `
	SELECT prefix, prefixes.description
	FROM prefixes INNER JOIN realms USING (realm_id)
	WHERE realms.name = $1
	AND prefixIsInside($2, prefix)
	ORDER BY prefixLen(prefix) DESC limit 1
	`
	var pfx string
	if err := p.db.QueryRow(q, p.realm, p.Prefix.String()).Scan(&pfx, &p.Description); err != nil {
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
	rows, err := p.db.Query(q, p.realm, p.Prefix.String())
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
