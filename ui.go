package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
)

func subPrefixes(pfx *IPNet) []int {
	min, max := (*net.IPNet)(pfx).Mask.Size()
	ret := make([]int, 0, max-min)
	for ; min < max; min++ {
		ret = append(ret, min+1)
	}
	return ret
}

func compileTemplates() (*template.Template, error) {
	helpers := map[string]interface{}{
		"subPrefixes": subPrefixes,
	}
	ret := template.New("").Funcs(helpers)
	d, err := AssetDir("templates")
	if err != nil {
		return nil, err
	}
	for _, f := range d {
		b, err := Asset(fmt.Sprintf("templates/%s", f))
		if err != nil {
			return nil, err
		}
		if _, err = ret.New(f).Parse(string(b)); err != nil {
			return nil, err
		}
	}
	return ret, nil
}

func (s *server) serveTemplate(w http.ResponseWriter, r *http.Request, name string, val interface{}) {
	if *debug {
		var err error
		s.tmpl, err = compileTemplates()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	realms, err := s.listRealms()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	realmID, _ := realmID(r)
	var b bytes.Buffer
	if err = s.tmpl.ExecuteTemplate(&b, name+".html", val); err != nil {
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
	if err = s.tmpl.ExecuteTemplate(&b, "main.html", ctx); err != nil {
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
	if err = s.realmExists(realmID); err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	prefixID, _ := prefixID(r)
	pfx, err := s.listPrefixes(realmID, prefixID)
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
