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

type Host struct {
	Id          int64  `json:"id"`
	Hostname    string `json:"hostname"`
	Description string `json:"description"`
}

func hostID(r *http.Request) (int64, error) {
	return strconv.ParseInt(mux.Vars(r)["hostID"], 10, 64)
}

func (s *server) listHosts(realmID int64) ([]*Host, error) {
	q := `SELECT host_id, hostname, description FROM hosts WHERE realm_id=$1`
	rows, err := s.db.Query(q, realmID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ret := []*Host{}
	for rows.Next() {
		var h Host
		if err = rows.Scan(&h.Id, &h.Hostname, &h.Description); err != nil {
			return nil, err
		}
		ret = append(ret, &h)
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

	q := `INSERT INTO hosts (realm_id, hostname, description) VALUES ($1, $2, $3)`
	res, err := s.db.Exec(q, realmID, h.Hostname, h.Description)
	if err != nil {
		errorJSON(w, err)
		return
	}
	h.Id, err = res.LastInsertId()
	if err != nil {
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

	q := `UPDATE hosts SET hostname=$1, description=$2 WHERE realm_id=$3 AND host_id=$4`
	_, err = s.db.Exec(q, h.Hostname, h.Description, realmID, hostID)
	if err != nil {
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
	}
	serveJSON(w, struct{}{})
}

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
	RealmID     int64  `json:"realm_id"`
	Address     IP     `json:"address"`
	Description string `json:"description"`
}

func addrID(r *http.Request) (int64, error) {
	return strconv.ParseInt(mux.Vars(r)["addrID"], 10, 64)
}

func (s *server) listHostAddrs(w http.ResponseWriter, r *http.Request) {
	hostID, err := hostID(r)
	if err != nil {
		errorJSON(w, err)
		return
	}

	q := `SELECT addr_id, realm_id, address, description FROM host_addrs WHERE host_id=$1`
	rows, err := s.db.Query(q, hostID)
	if err != nil {
		errorJSON(w, err)
		return
	}
	defer rows.Close()

	ret := struct {
		Addrs []*HostAddress `json:"addresses"`
	}{
		[]*HostAddress{},
	}
	for rows.Next() {
		var a HostAddress
		var addr string
		if err = rows.Scan(&a.Id, &a.RealmID, &addr, &a.Description); err != nil {
			errorJSON(w, err)
			return
		}
		a.Address = IP(net.ParseIP(addr))
		ret.Addrs = append(ret.Addrs, &a)
	}
	if err = rows.Err(); err != nil {
		errorJSON(w, err)
		return
	}

	serveJSON(w, ret)
}

func (s *server) createHostAddr(w http.ResponseWriter, r *http.Request) {
	hostID, err := hostID(r)
	if err != nil {
		errorJSON(w, err)
		return
	}

	var a HostAddress
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		errorJSON(w, err)
		return
	}
	if !a.Address.Valid() {
		errorJSON(w, errors.New("Invalid address"))
		return
	}

	q := `INSERT INTO host_addrs (realm_id, host_id, address, description) VALUES ($1, $2, $3, $4)`
	res, err := s.db.Exec(q, a.RealmID, hostID, a.Address.String(), a.Description)
	if err != nil {
		errorJSON(w, err)
		return
	}
	a.Id, err = res.LastInsertId()
	if err != nil {
		errorJSON(w, err)
		return
	}
	ret := struct {
		Addr *HostAddress `json:"address"`
	}{
		&a,
	}
	serveJSON(w, ret)
}

func (s *server) editHostAddr(w http.ResponseWriter, r *http.Request) {
	addrID, err := addrID(r)
	if err != nil {
		errorJSON(w, err)
		return
	}

	var a HostAddress
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		errorJSON(w, err)
		return
	}
	if !a.Address.Valid() {
		errorJSON(w, errors.New("Invalid address"))
		return
	}

	q := `UPDATE host_addrs SET realm_id=$1, address=$2, description=$3 WHERE addr_id=$4`
	_, err = s.db.Exec(q, a.RealmID, a.Address.String(), a.Description, addrID)
	if err != nil {
		errorJSON(w, err)
		return
	}

	a.Id = addrID
	ret := struct {
		Addr *HostAddress `json:"address"`
	}{
		&a,
	}
	serveJSON(w, ret)
}

func (s *server) deleteHostAddr(w http.ResponseWriter, r *http.Request) {
	addrID, err := addrID(r)
	if err != nil {
		errorJSON(w, err)
		return
	}

	q := `DELETE FROM host_addrs WHERE addr_id=$1`
	if _, err := s.db.Exec(q, addrID); err != nil {
		errorJSON(w, err)
	}
	serveJSON(w, struct{}{})
}
