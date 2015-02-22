package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	db "github.com/danderson/gipam/database"
)

const (
	allocationPath = "/api/allocation/"
	hostPath       = "/api/host/"
)

func runServer(addr *net.TCPAddr, db *db.DB) {
	s := &Server{db}
	http.HandleFunc(allocationPath, s.Allocation)
	http.HandleFunc(hostPath, s.Host)
	http.Handle("/", http.FileServer(http.Dir("ui")))
	http.ListenAndServe(addr.String(), nil)
}

type Server struct {
	db *db.DB
}

func writeJSON(w http.ResponseWriter, obj interface{}) {
	f, err := json.Marshal(obj)
	if err != nil {
		log.Printf("Failed to convert %#v to json: %s", obj, err)
		http.Error(w, "Error while converting to JSON", 500)
		return
	}
	w.Write(f)
}

func (s *Server) Allocation(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		cidr := strings.TrimPrefix(r.URL.Path, allocationPath)
		if cidr == "" {
			writeJSON(w, s.db.Allocs)
		} else {
			_, net, err := net.ParseCIDR(cidr)
			if err != nil {
				http.Error(w, "Malformed CIDR prefix", 400)
				return
			}
			alloc := s.db.FindAllocation(&db.IPNet{net}, true)
			if alloc == nil {
				http.Error(w, fmt.Sprintf("Allocation %s does not exist", net), 404)
				return
			}
			writeJSON(w, alloc)
		}
	case "POST":
		if strings.TrimPrefix(r.URL.Path, allocationPath) != "" {
			http.Error(w, fmt.Sprintf("Can only POST new allocations to %s", allocationPath), 400)
			return
		}
		var req struct {
			Name  string
			Net   *db.IPNet
			Attrs map[string]string
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad JSON value in body", 400)
			return
		}
		if err := s.db.AddAllocation(req.Name, req.Net, req.Attrs); err != nil {
			http.Error(w, fmt.Sprintf("Allocation of %s failed: %s", req.Net, err), 500)
			return
		}
		if err := s.db.Save("db"); err != nil {
			http.Error(w, "Couldn't save change", 500)
			return
		}

		writeJSON(w, s.db.FindAllocation(req.Net, true))
	case "PUT":
		cidr := strings.TrimPrefix(r.URL.Path, allocationPath)
		if cidr == "" {
			http.Error(w, "Can only PUT to edit a specific prefix", 400)
			return
		}
		_, net, err := net.ParseCIDR(cidr)
		if err != nil {
			http.Error(w, "Malformed CIDR prefix", 400)
			return
		}
		var req struct {
			Name  string
			Attrs map[string]string
		}
		if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad JSON value in body", 400)
			return
		}

		alloc := s.db.FindAllocation(&db.IPNet{net}, true)
		if alloc == nil {
			http.Error(w, "Allocation %s does not exist", 404)
			return
		}
		alloc.Name = req.Name
		alloc.Attrs = req.Attrs

		if err := s.db.Save("db"); err != nil {
			http.Error(w, "Couldn't save change", 500)
			return
		}

		writeJSON(w, alloc)
	case "DELETE":
		cidr := strings.TrimPrefix(r.URL.Path, allocationPath)
		if cidr == "" {
			http.Error(w, "Can only DELETE to delete a specific prefix", 400)
			return
		}
		_, net, err := net.ParseCIDR(cidr)
		if err != nil {
			http.Error(w, "Malformed CIDR prefix", 400)
			return
		}
		reparent := r.URL.Query().Get("reparent") != ""

		alloc := s.db.FindAllocation(&db.IPNet{net}, true)
		if alloc == nil {
			http.Error(w, "Allocation %s does not exist", 404)
			return
		}

		if err := s.db.RemoveAllocation(alloc, reparent); err != nil {
			http.Error(w, "Allocation removal failed", 500)
		}

		if err := s.db.Save("db"); err != nil {
			http.Error(w, "Couldn't save change", 500)
			return
		}
	}
}

func (s *Server) Host(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		addr := strings.TrimPrefix(r.URL.Path, hostPath)
		if addr == "" {
			writeJSON(w, s.db.Hosts)
		} else {
			ip := net.ParseIP(addr)
			if ip == nil {
				http.Error(w, "Malformed IP address", 400)
				return
			}
			host := s.db.FindHost(ip)
			if host == nil {
				http.Error(w, fmt.Sprintf("Host with IP %s does not exist", ip), 404)
				return
			}
			writeJSON(w, host)
		}
	case "POST":
		if strings.TrimPrefix(r.URL.Path, hostPath) != "" {
			http.Error(w, fmt.Sprintf("Can only POST new hosts to %s", hostPath), 400)
			return
		}
		var req struct {
			Name  string
			Addrs []net.IP
			Attrs map[string]string
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad JSON value in body", 400)
			return
		}
		if err := s.db.AddHost(req.Name, req.Addrs, req.Attrs); err != nil {
			http.Error(w, fmt.Sprintf("Adding host %s failed: %s", req.Name, err), 500)
			return
		}
		if err := s.db.Save("db"); err != nil {
			http.Error(w, "Couldn't save change", 500)
			return
		}

		writeJSON(w, s.db.FindHost(req.Addrs[0]))
	case "PUT":
		addr := strings.TrimPrefix(r.URL.Path, hostPath)
		if addr == "" {
			http.Error(w, "Can only PUT to edit a specific host (by IP address)", 400)
			return
		}
		ip := net.ParseIP(addr)
		if ip == nil {
			http.Error(w, "Malformed IP address", 400)
			return
		}
		var req struct {
			Name  string
			Addrs []net.IP
			Attrs map[string]string
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad JSON value in body", 400)
			return
		}

		host := s.db.FindHost(ip)
		if host == nil {
			http.Error(w, fmt.Sprintf("Host with IP %s does not exist", ip), 404)
			return
		}

		// TODO: needs atomicity here, i.e. a way to roll back the partial edit.
		if err := s.db.RemoveHost(host); err != nil {
			http.Error(w, fmt.Sprintf("Editing host %s failed: %s", host.Name, err), 500)
		}
		if err := s.db.AddHost(req.Name, req.Addrs, req.Attrs); err != nil {
			http.Error(w, fmt.Sprintf("Editing host %s failed: %s", host.Name, err), 500)
		}
		if err := s.db.Save("db"); err != nil {
			http.Error(w, "Couldn't save change", 500)
			return
		}

		writeJSON(w, host)
		// case "DELETE":
		// 	cidr := strings.TrimPrefix(r.URL.Path, hostPath)
		// 	if cidr == "" {
		// 		http.Error(w, "Can only DELETE to delete a specific prefix", 400)
		// 		return
		// 	}
		// 	_, net, err := net.ParseCIDR(cidr)
		// 	if err != nil {
		// 		http.Error(w, "Malformed CIDR prefix", 400)
		// 		return
		// 	}
		// 	reparent := r.URL.Query().Get("reparent") != ""

		// 	alloc := s.db.FindExact(&db.IPNet{net})
		// 	if alloc == nil {
		// 		http.Error(w, "Allocation %s does not exist", 404)
		// 		return
		// 	}

		// 	if err := s.db.RemoveAllocation(alloc, reparent); err != nil {
		// 		http.Error(w, "Allocation removal failed", 500)
		// 	}

		// 	if err := db.Save("db", s.db); err != nil {
		// 		http.Error(w, "Couldn't save change", 500)
		// 		return
		// 	}
	}
}
