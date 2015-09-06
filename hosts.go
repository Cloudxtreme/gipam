package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

type IP net.IP

func (ip IP) MarshalJSON() ([]byte, error) {
	if !ip.Valid() {
		return nil, fmt.Errorf("Invalid IP %q", ip)
	}
	return []byte(fmt.Sprintf("%q", ip.String())), nil
}

func (ip IP) UnmarshalJSON(b []byte) error {
	var addr string
	if err := json.Unmarshal(b, &addr); err != nil {
		return err
	}
	ret := IP(net.ParseIP(addr))
	if ret == nil {
		return fmt.Errorf("Invalid IP %q", addr)
	}
	ip = ret
	return nil
}

func (ip IP) String() string {
	return (net.IP)(ip).String()
}

func (ip IP) Valid() bool {
	return (net.IP)(ip).To16() != nil
}

type HostAddress struct {
	Id          int64  `json:"id"`
	RealmID     int64  `json:"realm_id,omitempty"`
	IP          IP     `json:"address"`
	Description string `json:"description"`
}

type Host struct {
	Id          int64          `json:"id"`
	Hostname    string         `json:"hostname"`
	Description string         `json:"description"`
	Addrs       []*HostAddress `json:"addresses"`
}

func hostID(r *http.Request) (int64, error) {
	return strconv.ParseInt(mux.Vars(r)["hostID"], 10, 64)
}

func addrID(r *http.Request) (int64, error) {
	return strconv.ParseInt(mux.Vars(r)["addrID"], 10, 64)
}

func (s *server) listHosts(realmID int64) ([]*Host, error) {
	q := `
SELECT hosts.host_id, hosts.hostname, hosts.description,
       host_addrs.addr_id, host_addrs.realm_id, host_addrs.address, host_addrs.description
FROM hosts INNER JOIN host_addrs USING (host_id)
WHERE hosts.realm_id=$1
ORDER BY hosts.host_id, host_addrs.addr_id
`
	rows, err := s.db.Query(q, realmID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ret := []*Host{}
	// Position of host_id within ret
	hostIdx := map[int64]int{}

	for rows.Next() {
		var h Host
		var a HostAddress
		if err = rows.Scan(&h.Id, &h.Hostname, &h.Description, &a.Id, &a.RealmID, &a.IP, &a.Description); err != nil {
			return nil, err
		}
		if off, ok := hostIdx[h.Id]; ok {
			ret[off].Addrs = append(ret[off].Addrs, &a)
		} else {
			h.Addrs = []*HostAddress{&a}
			ret = append(ret, &h)
			hostIdx[h.Id] = len(ret) - 1
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return ret, nil
}

func (s *server) createHost(w http.ResponseWriter, r *http.Request) {
	realmID, err := realmID(r)
	if err != nil {
		errorJSON(w, err)
		return
	}

	var h Host
	if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
		errorJSON(w, err)
		return
	}

	if h.Hostname == "" || len(h.Addrs) == 0 {
		errorJSON(w, errors.New("Incomplete host spec"))
	}

	tx, err := s.db.Begin()
	if err != nil {
		errorJSON(w, err)
		return
	}
	defer tx.Rollback()

	q := `INSERT INTO hosts (realm_id, hostname, description) VALUES ($1, $2, $3)`
	res, err := tx.Exec(q, realmID, h.Hostname, h.Description)
	if err != nil {
		errorJSON(w, err)
		return
	}
	h.Id, err = res.LastInsertId()
	if err != nil {
		errorJSON(w, err)
		return
	}

	q = `INSERT INTO host_addrs (realm_id, host_id, address, description) VALUES ($1, $2, $3, $4)`
	for _, a := range h.Addrs {
		res, err := tx.Exec(q, a.RealmID, h.Id, a.IP, a.Description)
		if err != nil {
			errorJSON(w, err)
			return
		}
		a.Id, err = res.LastInsertId()
		if err != nil {
			errorJSON(w, err)
			return
		}
	}

	if err = tx.Commit(); err != nil {
		errorJSON(w, err)
		return
	}

	ret := struct {
		Host *Host `json:"host"`
	}{
		&h,
	}
	serveJSON(w, ret)
}

func (s *server) editHost(w http.ResponseWriter, r *http.Request) {
	realmID, err := realmID(r)
	if err != nil {
		errorJSON(w, err)
		return
	}

	hostID, err := hostID(r)
	if err != nil {
		errorJSON(w, err)
		return
	}

	var h Host
	if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
		errorJSON(w, err)
		return
	}

	if h.Hostname == "" || len(h.Addrs) == 0 {
		errorJSON(w, errors.New("Incomplete host spec"))
	}

	tx, err := s.db.Begin()
	if err != nil {
		errorJSON(w, err)
		return
	}
	defer tx.Rollback()

	q := `UPDATE hosts SET hostname=$1, description=$2 WHERE realm_id=$3 AND host_id=$4`
	_, err = tx.Exec(q, h.Hostname, h.Description, realmID, hostID)
	if err != nil {
		errorJSON(w, err)
		return
	}

	q = `SELECT addr_id, realm_id, address FROM host_addrs WHERE host_id=$1`
	rows, err := tx.Query(q, h.Id)
	if err != nil {
		errorJSON(w, err)
		return
	}
	defer rows.Close()

	existingAddrs := map[string]int64{}
	for rows.Next() {
		var addrID, realmID int64
		var ip string
		if err = rows.Scan(&addrID, &realmID, &ip); err != nil {
			errorJSON(w, err)
			return
		}
		existingAddrs[fmt.Sprintf("%d/%s", realmID, ip)] = addrID
	}

	for _, a := range h.Addrs {
		id, ok := existingAddrs[fmt.Sprintf("%d/%s", a.RealmID, a.IP)]
		if ok {
			// Address already in DB, just update the description
			q = `UPDATE host_addrs SET description=$1 WHERE addr_id=$2`
			if _, err = tx.Exec(q, a.Description, id); err != nil {
				errorJSON(w, err)
				return
			}
			delete(existingAddrs, fmt.Sprintf("%d/%s", a.RealmID, a.IP))
		} else {
			// New address.
			q = `INSERT INTO host_addrs (realm_id, host_id, address, description) VALUES ($1, $2, $3, $4)`
			if _, err = tx.Exec(q, a.RealmID, h.Id, a.IP.String(), a.Description); err != nil {
				errorJSON(w, err)
				return
			}
		}
	}

	// Entries left in existingAddrs are to be deleted
	for _, id := range existingAddrs {
		q = `DELETE FROM host_addrs WHERE addr_id=$1`
		if _, err := tx.Exec(q, id); err != nil {
			errorJSON(w, err)
			return
		}
	}

	if err = tx.Commit(); err != nil {
		errorJSON(w, err)
		return
	}

	h.Id = hostID
	ret := struct {
		Host *Host `json:"host"`
	}{
		&h,
	}
	serveJSON(w, ret)
}

func (s *server) deleteHost(w http.ResponseWriter, r *http.Request) {
	realmID, err := realmID(r)
	if err != nil {
		errorJSON(w, err)
	}

	hostID, err := hostID(r)
	if err != nil {
		errorJSON(w, err)
		return
	}

	q := `DELETE FROM hosts WHERE realm_id=$1 AND host_id=$2`
	if _, err := s.db.Exec(q, realmID, hostID); err != nil {
		errorJSON(w, err)
		return
	}
	serveJSON(w, struct{}{})
}
