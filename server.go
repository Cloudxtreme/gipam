package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/gorilla/mux"
)

func runServer(addr string, dbPath string) error {
	db, err := NewDB(dbPath)
	if err != nil {
		return err
	}

	s := &server{
		dbPath: dbPath,
		db:     db,
		mux:    mux.NewRouter(),
	}

	s.registerAPI()
	s.mux.Path("/realm/create").HandlerFunc(s.createRealmUI)
	s.mux.Path("/realm/{RealmID:[0-9]+}/delete").HandlerFunc(s.deleteRealmUI)

	s.mux.Path("/realm/{RealmID:[0-9]+}/prefixes").HandlerFunc(s.listPrefixesUI)
	s.mux.Path("/realm/{RealmID:[0-9]+}/prefixes/{PrefixID:[0-9]+}").HandlerFunc(s.listPrefixesUI)

	s.mux.Path("/gipam.css").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := ioutil.ReadFile("gipam.css")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		w.Write(b)
	})

	s.mux.Path("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		realms, err := s.listRealms()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if len(realms) == 0 {
			// No realms - we probably want one.
			http.Redirect(w, r, "/realm/create", 302)
			return
		} else {
			http.Redirect(w, r, fmt.Sprintf("/realm/%d/prefixes", realms[0].Id), 302)
		}
		w.Write([]byte("Placeholder handler"))
	})

	s.mux.Path("/resetDB").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.db.Close()
		if s.dbPath != ":memory:" {
			if err := os.Remove(s.dbPath); err != nil {
				http.Error(w, fmt.Sprintf("Failed to delete DB: %s. I will probably crash soon.", err), 500)
				return
			}
		}
		db, err := NewDB(dbPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to recreate DB: %s. I will probably crash soon.", err), 500)
			return
		}
		s.db = db
		http.Redirect(w, r, "/realm/create", 302)
	})

	return http.ListenAndServe(addr, s.mux)
}

type server struct {
	dbPath string
	db     *sql.DB
	mux    *mux.Router
}

type api struct {
	db *sql.DB
}

func (s *server) registerAPI() {
	api := s.mux.PathPrefix("/api").Subrouter()

	api.Path("/realms").Methods("POST").HandlerFunc(s.createRealm)
	api.Path("/realms/{RealmID:[0-9]+}").Methods("PUT").HandlerFunc(s.editRealm)
	api.Path("/realms/{RealmID:[0-9]+}").Methods("DELETE").HandlerFunc(s.deleteRealm)

	api.Path("/realms/{RealmID:[0-9]+}/prefixes").Methods("POST").HandlerFunc(s.createPrefix)
	api.Path("/realms/{RealmID:[0-9]+}/prefixes/{PrefixID:[0-9]+}").Methods("PUT").HandlerFunc(s.editPrefix)
	api.Path("/realms/{RealmID:[0-9]+}/prefixes/{PrefixID:[0-9]+}").Methods("DELETE").HandlerFunc(s.deletePrefix)

	api.Path("/realms/{RealmID:[0-9]+}/hosts").Methods("POST").HandlerFunc(s.createHost)
	api.Path("/realms/{RealmID:[0-9]+}/hosts/{HostID:[0-9]+}").Methods("PUT").HandlerFunc(s.editHost)
	api.Path("/realms/{RealmID:[0-9]+}/hosts/{HostID:[0-9]+}").Methods("DELETE").HandlerFunc(s.deleteHost)

	api.Path("/realms/{RealmID:[0-9]+}/hosts/{HostID:[0-9]+}/addresses").Methods("POST").HandlerFunc(s.createHostAddr)
	api.Path("/realms/{RealmID:[0-9]+}/hosts/{HostID:[0-9]+}/addresses/{AddrID:[0-9]+}").Methods("PUT").HandlerFunc(s.editHostAddr)
	api.Path("/realms/{RealmID:[0-9]+}/hosts/{HostID:[0-9]+}/addresses/{AddrID:[0-9]+}").Methods("DELETE").HandlerFunc(s.deleteHostAddr)
}

func marshalJSON(val interface{}) ([]byte, error) {
	if *debug {
		return json.MarshalIndent(val, "", "  ")
	}
	return json.Marshal(val)
}

func serveJSON(w http.ResponseWriter, val interface{}) {
	b, err := marshalJSON(val)
	if err != nil {
		errorJSON(w, err)
		return
	}
	w.Write(b)
}

func errorJSON(w http.ResponseWriter, err error) {
	ret := struct {
		Error string `json:"error"`
	}{
		err.Error(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(500)

	b, err := marshalJSON(ret)
	if err != nil {
		w.Write([]byte(`{error: "got an error while marhsalling error"}`))
		return
	}
	w.Write(b)
}
