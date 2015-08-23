package db

import (
	"database/sql"
	"fmt"
	"net"
)

type Host struct {
	db          *sql.DB
	realm       string
	Hostname    string
	Description string
}

func (h *Host) Create() error {
	q := `
INSERT INTO hosts (realm_id, hostname, description)
VALUES ((SELECT realm_id FROM realms WHERE name=$1), $2, $3)
`
	_, err := h.db.Exec(q, h.realm, h.Hostname, h.Description)
	if err != nil && errIsAlreadyExists(err) {
		return ErrAlreadyExists
	}
	return err
}

func (h *Host) Save() error {
	q := `
UPDATE hosts
SET description=$1
WHERE realm_id=(SELECT realm_id FROM realms WHERE name=$2) AND hostname=$3
`
	res, err := h.db.Exec(q, h.Description, h.realm, h.Hostname)
	if err != nil {
		return err
	}
	return mustHaveChanged(res)
}

func (h *Host) Delete() error {
	q := `
DELETE FROM hosts
WHERE realm_id=(SELECT realm_id FROM realms WHERE name=$1) AND hostname=$2
`
	if _, err := h.db.Exec(q, h.realm, h.Hostname); err != nil {
		return err
	}
	return nil
}

func (h *Host) Get() error {
	q := `
SELECT hosts.description
FROM hosts INNER JOIN realms USING (realm_id)
WHERE realms.name=$1 AND hostname=$2
`
	if err := h.db.QueryRow(q, h.realm, h.Hostname).Scan(&h.Description); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (h *Host) AddAddress(ip net.IP) error {
	q := `
INSERT INTO host_addrs (realm_id, host_id, address)
VALUES (
  (SELECT realm_id FROM realms WHERE name=$1),
  (SELECT host_id FROM hosts INNER JOIN realms USING (realm_id) WHERE realms.name=$1 AND hostname=$2),
  $3
)
`
	_, err := h.db.Exec(q, h.realm, h.Hostname, ip.String())
	if err != nil && errIsAlreadyExists(err) {
		return ErrAlreadyExists
	}
	return err
}

func (h *Host) DeleteAddress(ip net.IP) error {
	q := `
DELETE FROM host_addrs
WHERE host_id=(SELECT host_id FROM hosts INNER JOIN realms USING (realm_id) WHERE realms.name=$1 AND hostname=$2)
AND address=$3
`
	if _, err := h.db.Exec(q, h.realm, h.Hostname, ip.String()); err != nil {
		return err
	}
	return nil
}

func (h *Host) Addresses() ([]net.IP, error) {
	q := `
SELECT address
FROM host_addrs INNER JOIN hosts USING (host_id) INNER JOIN realms USING (realm_id)
WHERE realms.name=$1 AND hosts.hostname=$2
`
	rows, err := h.db.Query(q, h.realm, h.Hostname)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ret []net.IP
	for rows.Next() {
		var a string
		if err = rows.Scan(&a); err != nil {
			return nil, err
		}
		ip := net.ParseIP(a)
		if ip == nil {
			return nil, fmt.Errorf("Malformed IP address %q", ip)
		}
		ret = append(ret, ip)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return ret, nil
}
