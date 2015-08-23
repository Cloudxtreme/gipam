package db

import (
	"database/sql"
	"encoding/binary"
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
				if err := conn.RegisterFunc("isSubnetOf", dbIsSubnetOf, true); err != nil {
					return err
				}
				if err := conn.RegisterFunc("prefixLen", dbPrefixLen, true); err != nil {
					return err
				}
				if err := conn.RegisterFunc("prefixAsInt", dbPrefixAsInt, true); err != nil {
					return err
				}
				return nil
			},
		})
}

// dbIsSubnetOf returns true if child is a subnet of parent, or is equal to parent.
func dbIsSubnetOf(parent, child string) (bool, error) {
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

func dbPrefixAsInt(pfx string, upper bool, max bool) (uint64, error) {
	_, n, err := net.ParseCIDR(pfx)
	if err != nil {
		return 0, err
	}
	if len(n.IP) != len(n.Mask) {
		panic("Incoherent IP/mask")
	}
	if max {
		for i := range n.IP {
			n.IP[i] |= 0xff & ^n.Mask[i]
		}
	}
	ip := n.IP.To16()
	if upper {
		return binary.BigEndian.Uint64(ip[:8]), nil
	} else {
		return binary.BigEndian.Uint64(ip[8:]), nil
	}
}
