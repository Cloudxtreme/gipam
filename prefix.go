package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"

	"github.com/gorilla/mux"
)

type IPNet net.IPNet

func (n *IPNet) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", n)), nil
}

func (n *IPNet) UnmarshalJSON(b []byte) error {
	var pfx string
	if err := json.Unmarshal(b, &pfx); err != nil {
		return err
	}
	_, ret, err := net.ParseCIDR(pfx)
	if err != nil {
		return err
	}
	*n = *(*IPNet)(ret)
	return nil
}

func (n *IPNet) String() string {
	return (*net.IPNet)(n).String()
}

type Prefix struct {
	Id          int64  `json:"id"`
	Prefix      *IPNet `json:"prefix"`
	Description string `json:"description"`
}

type PrefixTree struct {
	Prefix
	Depth    int64         `json:"depth"`
	Children []*PrefixTree `json:"children"`
}

func prefixID(r *http.Request) (int64, error) {
	return strconv.ParseInt(mux.Vars(r)["PrefixID"], 10, 64)
}

func (s *server) listPrefixes(realmID, prefixID int64) (roots []*PrefixTree, err error) {
	var rows *sql.Rows
	if prefixID > 0 {
		q := `
WITH RECURSIVE pfx(prefix_id, parent_id, prefix, description) AS (
  SELECT prefix_id, NULL, prefix, description
  FROM prefixes
  WHERE realm_id=$1 AND prefix_id=$2
UNION ALL
  SELECT prefixes.prefix_id, prefixes.parent_id, prefixes.prefix, prefixes.description
  FROM prefixes, pfx
  WHERE prefixes.parent_id = pfx.prefix_id
)
SELECT prefix_id, parent_id, prefix, description
FROM pfx
`
		rows, err = s.db.Query(q, realmID, prefixID)
	} else {
		q := `SELECT prefix_id, parent_id, prefix, description FROM prefixes WHERE realm_id=$1`
		rows, err = s.db.Query(q, realmID)
	}
	if err != nil {
		return nil, err
	}

	prefixes := map[int64]*PrefixTree{}
	parents := map[int64]int64{}
	roots = []*PrefixTree{}
	for rows.Next() {
		pfx := PrefixTree{
			Children: []*PrefixTree{},
		}
		var pfxStr string
		var parentID *int64
		if err := rows.Scan(&pfx.Id, &parentID, &pfxStr, &pfx.Description); err != nil {
			return nil, err
		}

		_, n, err := net.ParseCIDR(pfxStr)
		if err != nil {
			return nil, err
		}
		pfx.Prefix.Prefix = (*IPNet)(n)
		if parentID == nil {
			roots = append(roots, &pfx)
		} else {
			parents[pfx.Id] = *parentID
		}
		prefixes[pfx.Id] = &pfx
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for id, parentID := range parents {
		prefixes[parentID].Children = append(prefixes[parentID].Children, prefixes[id])
	}

	markDepth(roots, 0)

	return roots, nil
}

func markDepth(pt []*PrefixTree, depth int64) {
	sort.Sort(prefixTreeSorter(pt))
	for _, p := range pt {
		p.Depth = depth
		fmt.Println(p.Prefix, p.Depth)
		markDepth(p.Children, depth+1)
	}
}

type prefixTreeSorter []*PrefixTree

func (p prefixTreeSorter) Len() int {
	return len(p)
}

func (p prefixTreeSorter) Less(a, b int) bool {
	return bytes.Compare(p[a].Prefix.Prefix.IP, p[b].Prefix.Prefix.IP) < 0
}

func (p prefixTreeSorter) Swap(a, b int) {
	p[a], p[b] = p[b], p[a]
}

func (s *server) createPrefix(w http.ResponseWriter, r *http.Request) {
	realmID, err := realmID(r)
	if err != nil {
		errorJSON(w, err)
		return
	}

	var pfx Prefix
	if err := json.NewDecoder(r.Body).Decode(&pfx); err != nil {
		errorJSON(w, err)
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		errorJSON(w, err)
		return
	}
	defer tx.Rollback()

	var parentID *int64
	q := `SELECT prefix_id FROM prefixes WHERE realm_id=$1 AND prefixIsInside($2, prefix) ORDER BY prefixLen(prefix) DESC LIMIT 1`
	err = tx.QueryRow(q, realmID, pfx.Prefix.String()).Scan(&parentID)
	if err != nil && err != sql.ErrNoRows {
		errorJSON(w, err)
		return
	}

	q = `
INSERT INTO prefixes (realm_id, parent_id, prefix, description)
VALUES ($1, $2, $3, $4)`
	res, err := tx.Exec(q, realmID, parentID, pfx.Prefix.String(), pfx.Description)
	if err != nil {
		errorJSON(w, err)
		return
	}

	pfx.Id, err = res.LastInsertId()
	if err != nil {
		errorJSON(w, err)
		return
	}

	if parentID == nil {
		q = `
UPDATE prefixes SET parent_id=$1
WHERE realm_id=$2
AND parent_id IS NULL AND prefixIsInside(prefix, $3)
`
		if _, err = tx.Exec(q, pfx.Id, realmID, pfx.Prefix.String()); err != nil {
			errorJSON(w, err)
			return
		}
	} else {
		q = `
UPDATE prefixes SET parent_id=$1
WHERE realm_id=$2
AND parent_id=$3 AND prefixIsInside(prefix, $4)
`
		if _, err = tx.Exec(q, pfx.Id, realmID, *parentID, pfx.Prefix.String()); err != nil {
			errorJSON(w, err)
			return
		}
	}

	if err = tx.Commit(); err != nil {
		errorJSON(w, err)
		return
	}

	serveJSON(w, pfx)
}

func (s *server) editPrefix(w http.ResponseWriter, r *http.Request) {
	realmID, err := realmID(r)
	if err != nil {
		errorJSON(w, err)
		return
	}

	prefixID, err := prefixID(r)
	if err != nil {
		errorJSON(w, err)
		return
	}

	var pfx Prefix
	if err := json.NewDecoder(r.Body).Decode(&pfx); err != nil {
		errorJSON(w, err)
		return
	}

	q := `UPDATE prefixes SET description=$1 WHERE realm_id=$2 AND prefix_id=$3`
	_, err = s.db.Exec(q, pfx.Description, realmID, prefixID)
	if err != nil {
		errorJSON(w, err)
		return
	}

	pfx.Id = prefixID
	ret := struct {
		Prefix *Prefix `json:"prefix"`
	}{
		&pfx,
	}
	serveJSON(w, ret)
}

func (s *server) deletePrefix(w http.ResponseWriter, r *http.Request) {
	realmID, err := realmID(r)
	if err != nil {
		errorJSON(w, err)
	}

	prefixID, err := prefixID(r)
	if err != nil {
		errorJSON(w, err)
		return
	}

	_, recursive := r.URL.Query()["recursive"]

	tx, err := s.db.Begin()
	if err != nil {
		errorJSON(w, err)
		return
	}
	defer tx.Rollback()

	if !recursive {
		// To avoid a cascading delete, we need to reparent explicitly
		// first.
		q := `UPDATE prefixes SET parent_id=(SELECT parent_id FROM prefixes WHERE realm_id=$1 AND prefix_id=$2) WHERE realm_id=$1 AND parent_id=$2`
		if _, err := s.db.Exec(q, realmID, prefixID); err != nil {
			errorJSON(w, err)
			return
		}
	}

	// ON DELETE CASCADE takes care of nuking the children in the
	// recursive case.
	q := `DELETE FROM prefixes WHERE realm_id=$1 AND prefix_id=$2`
	if _, err := s.db.Exec(q, realmID, prefixID); err != nil {
		errorJSON(w, err)
		return
	}

	if err = tx.Commit(); err != nil {
		errorJSON(w, err)
		return
	}
	serveJSON(w, struct{}{})
}
