package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
)

func makeMap(vals ...interface{}) (ret map[string]interface{}, err error) {
	if len(vals)%2 != 0 {
		return nil, errors.New("makeMap must be given an even number of arguments")
	}
	ret = map[string]interface{}{}
	for len(vals) > 0 {
		k, ok := vals[0].(string)
		v := vals[1]
		if !ok {
			return nil, errors.New("Every other makeMap argument must be a string")
		}
		ret[k] = v
		vals = vals[2:]
	}
	return ret, nil
}

func subPrefixes(pfx *IPNet) []int {
	min, max := (*net.IPNet)(pfx).Mask.Size()
	ret := make([]int, 0, max-min)
	for ; min < max; min++ {
		ret = append(ret, min+1)
	}
	return ret
}

func (s *server) serveTemplate(w http.ResponseWriter, r *http.Request, name string, val interface{}) {
	helpers := map[string]interface{}{
		"makeMap":     makeMap,
		"subPrefixes": subPrefixes,
	}
	tmpl, err := template.New("").Funcs(helpers).ParseGlob("templates/*.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	realms, err := s.listRealms()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	realmID, _ := realmID(r)
	var b bytes.Buffer
	if err = tmpl.ExecuteTemplate(&b, name+".html", val); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	ctx := struct {
		Realms        []*Realm
		SelectedRealm *Realm
		Page          template.HTML
		Debug         bool
	}{
		Realms:        realms,
		SelectedRealm: nil,
		Page:          template.HTML(b.String()),
		Debug:         *debug,
	}
	if realmID > 0 {
		for _, r := range realms {
			if r.Id == realmID {
				ctx.SelectedRealm = r
				break
			}
		}
	}
	b.Reset()
	if err = tmpl.ExecuteTemplate(&b, "main.html", ctx); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	io.Copy(w, &b)
}

func (s *server) createRealmUI(w http.ResponseWriter, r *http.Request) {
	s.serveTemplate(w, r, "createRealm", nil)
}

func (s *server) deleteRealmUI(w http.ResponseWriter, r *http.Request) {
	realmID, err := realmID(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	realms, err := s.listRealms()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	ctx := struct {
		Id   int64
		Name string
	}{
		Id: realmID,
	}
	for _, r := range realms {
		if r.Id == realmID {
			ctx.Name = r.Name
		}
	}
	s.serveTemplate(w, r, "deleteRealm", ctx)
}

func (s *server) listPrefixesUI(w http.ResponseWriter, r *http.Request) {
	realmID, err := realmID(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	prefixID, _ := prefixID(r)
	fmt.Println(realmID, prefixID)
	pfx, err := s.listPrefixes(realmID, prefixID)
	fmt.Printf("%#v\n", pfx)
	s.serveTemplate(w, r, "listPrefixes", struct {
		RealmID  int64
		Prefixes []*PrefixTree
	}{realmID, pfx})
}

func (s *server) createPrefixUI(w http.ResponseWriter, r *http.Request) {
	realmID, err := realmID(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.serveTemplate(w, r, "createPrefix", realmID)
}
