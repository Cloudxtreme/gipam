package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	db "github.com/danderson/gipam/database"
)

func main() {
	d, err := db.Load("db")
	if err != nil {
		if !os.IsNotExist(err) {
			log.Fatalln(err)
		}
		log.Printf("Alloc file doesn't exist, creating empty alloc")
		d = &db.DB{
			Name: "New Universe",
		}
	}
	// Resave immediately, in case there are any format upgrades.
	if err = db.Save("db", d); err != nil {
		log.Fatalln(err)
	}
	s := &Server{d}

	http.HandleFunc("/api/allocate", s.Allocate)
	http.HandleFunc("/api/deallocate", s.Deallocate)
	http.HandleFunc("/api/edit", s.Edit)
	http.HandleFunc("/api/list", s.List)
	http.Handle("/", http.FileServer(http.Dir("ui")))
	http.ListenAndServe(":8080", nil)
}

type Server struct {
	db *db.DB
}

func printAlloc(w io.Writer, a *db.Allocation, indent string) {
	net := a.Net.String()
	if a.IsHost() {
		net = a.Net.IP.String()
	}
	fmt.Fprintf(w, "%s%s %s\n", indent, a.Name, net)
	for _, a := range a.Children {
		printAlloc(w, a, indent+"  ")
	}
}

func (s *Server) Home(w http.ResponseWriter, r *http.Request) {
	for _, a := range s.db.Allocs {
		printAlloc(w, a, "")
	}
}

func (s *Server) Allocate(w http.ResponseWriter, r *http.Request) {
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Fprintf(w, "Error: %s", err)
		return
	}
	var req struct {
		Net   *db.IPNet
		Name  string
		Attrs map[string]string
	}
	if err = json.Unmarshal(b, &req); err != nil {
		fmt.Fprintf(w, "Error: %s", err)
		return
	}

	if err = s.db.Allocate(req.Name, req.Net, req.Attrs); err != nil {
		fmt.Fprintf(w, "Error: %s", err)
		return
	}

	if err = db.Save("db", s.db); err != nil {
		fmt.Fprintf(w, "Error: %s", err)
		return
	}
	return
}

func (s *Server) Deallocate(w http.ResponseWriter, r *http.Request) {
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Fprintf(w, "Error: %s", err)
		return
	}
	var req struct {
		Net      *db.IPNet
		Reparent bool
	}
	if err = json.Unmarshal(b, &req); err != nil {
		fmt.Fprintf(w, "Error: %s", err)
		return
	}

	a := s.db.FindExact(req.Net)
	if a == nil {
		fmt.Fprintf(w, "Error: allocation %s not found", req.Net)
		return
	}

	if err = s.db.Deallocate(a, req.Reparent); err != nil {
		fmt.Fprintf(w, "Error: %s", err)
		return
	}

	if err = db.Save("db", s.db); err != nil {
		fmt.Fprintf(w, "Error: %s", err)
	}
}

func (s *Server) Edit(w http.ResponseWriter, r *http.Request) {
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Fprintf(w, "Error: %s", err)
		return
	}
	var req struct {
		Net   *db.IPNet
		Name  string
		Attrs map[string]string
	}
	if err = json.Unmarshal(b, &req); err != nil {
		fmt.Fprintf(w, "Error: %s", err)
		return
	}

	a := s.db.FindExact(req.Net)
	if a == nil {
		fmt.Fprintf(w, "Error: allocation %s not found", req.Net)
		return
	}

	a.Name = req.Name
	a.Attrs = req.Attrs

	if err = db.Save("db", s.db); err != nil {
		fmt.Fprintf(w, "Error: %s", err)
	}
}

func (s *Server) List(w http.ResponseWriter, r *http.Request) {
	f, err := json.Marshal(s.db)
	if err != nil {
		fmt.Fprintf(w, "Error: %s", err)
		return
	}
	w.Write(f)
}
