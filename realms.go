package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

type Realm struct {
	Id          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func realmID(r *http.Request) (int64, error) {
	return strconv.ParseInt(mux.Vars(r)["RealmID"], 10, 64)
}

func (s *server) listRealms() (ret []*Realm, err error) {
	q := `SELECT realm_id, name, description FROM realms ORDER BY name`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var r Realm
		if err = rows.Scan(&r.Id, &r.Name, &r.Description); err != nil {
			return nil, err
		}
		ret = append(ret, &r)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return ret, nil
}

func (s *server) createRealm(w http.ResponseWriter, r *http.Request) {
	var realm Realm
	var b []byte
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		errorJSON(w, err)
	}
	if err := json.Unmarshal(b, &realm); err != nil {
		errorJSON(w, err)
		return
	}

	if realm.Name == "" {
		errorJSON(w, errors.New("Must specify a realm name."))
		return
	}

	q := `INSERT INTO realms (name, description) VALUES ($1, $2)`
	res, err := s.db.Exec(q, realm.Name, realm.Description)
	if err != nil {
		errorJSON(w, err)
		return
	}
	realm.Id, err = res.LastInsertId()
	if err != nil {
		errorJSON(w, err)
		return
	}
	ret := struct {
		Realm *Realm `json:"realm"`
	}{
		&realm,
	}
	serveJSON(w, ret)
}

func (s *server) editRealm(w http.ResponseWriter, r *http.Request) {
	var realm Realm
	err := json.NewDecoder(r.Body).Decode(&realm)
	if err != nil {
		errorJSON(w, err)
		return
	}
	realm.Id, err = realmID(r)
	if err != nil {
		errorJSON(w, err)
	}

	q := `UPDATE realms SET name=$1, description=$2 WHERE realm_id=$3`
	_, err = s.db.Exec(q, realm.Name, realm.Description, realm.Id)
	if err != nil {
		errorJSON(w, err)
	}

	ret := struct {
		Realm *Realm `json:"realm"`
	}{
		&realm,
	}
	serveJSON(w, ret)
}

func (s *server) deleteRealm(w http.ResponseWriter, r *http.Request) {
	id, err := realmID(r)
	if err != nil {
		errorJSON(w, err)
	}

	q := `DELETE FROM realms WHERE realm_id=$1`
	if _, err := s.db.Exec(q, id); err != nil {
		errorJSON(w, err)
	}
	serveJSON(w, struct{}{})
}
