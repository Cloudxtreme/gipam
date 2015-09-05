package main

import (
	"database/sql"
	"net"

	sqlite "github.com/mattn/go-sqlite3"

	"github.com/danderson/gipam/util"
)

// Custom IPAM-oriented functions exposed to SQLite, to enable cool
// queries.

func init() {
	sql.Register("sqlite3_gipam",
		&sqlite.SQLiteDriver{
			ConnectHook: func(conn *sqlite.SQLiteConn) error {
				if err := conn.RegisterFunc("prefixIsInside", dbPrefixIsInside, true); err != nil {
					return err
				}
				if err := conn.RegisterFunc("prefixLen", dbPrefixLen, true); err != nil {
					return err
				}
				return nil
			},
		})
}

// Return true if child is a strict subnet of parent.
//
// "Strict" means that dbPrefixIsInside(a, a) is False, so you need to
// add an equality test to your query if you want identity matches.
func dbPrefixIsInside(child, parent string) (bool, error) {
	_, n1, err := net.ParseCIDR(parent)
	if err != nil {
		return false, err
	}
	_, n2, err := net.ParseCIDR(child)
	if err != nil {
		return false, err
	}

	return util.PrefixContains(n1, n2), nil
}

// dbPrefixLen returns the length of the given prefix.
func dbPrefixLen(pfx string) (int, error) {
	_, n, err := net.ParseCIDR(pfx)
	if err != nil {
		return 0, err
	}
	l, _ := n.Mask.Size()
	return l, nil
}
