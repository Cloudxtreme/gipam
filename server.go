package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	db "github.com/danderson/gipam/database"
	"github.com/danderson/gipam/export/bind9"
)

const (
	subnetPath = "/api/subnet/"
	hostPath   = "/api/host/"
	bind9Path  = "/api/export/bind9/"
)

func runServer(addr string, db *db.DB) {
	s := &server{db}
	http.HandleFunc(subnetPath, s.Subnet)
	http.HandleFunc(hostPath, s.Host)
	http.HandleFunc(bind9Path, s.Bind9)
	http.Handle("/", http.FileServer(http.Dir("ui")))
	http.ListenAndServe(addr, nil)
}

type server struct {
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

func (s *server) Subnet(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		cidr := strings.TrimPrefix(r.URL.Path, subnetPath)
		if cidr == "" {
			writeJSON(w, s.db.Subnets)
		} else {
			_, net, err := net.ParseCIDR(cidr)
			if err != nil {
				http.Error(w, "Malformed CIDR prefix", 400)
				return
			}
			subnet := s.db.Subnet(net, true)
			if subnet == nil {
				http.Error(w, fmt.Sprintf("Subnet %s does not exist", net), 404)
				return
			}
			writeJSON(w, subnet)
		}
	case "POST":
		if strings.TrimPrefix(r.URL.Path, subnetPath) != "" {
			http.Error(w, fmt.Sprintf("Can only POST new subnets to %s", subnetPath), 400)
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
		if err := s.db.AddSubnet(req.Name, (*net.IPNet)(req.Net), req.Attrs); err != nil {
			http.Error(w, fmt.Sprintf("Allocation of %s failed: %s", req.Net, err), 500)
			return
		}
		if err := s.db.Save(); err != nil {
			http.Error(w, "Couldn't save change", 500)
			return
		}

		writeJSON(w, s.db.Subnet((*net.IPNet)(req.Net), true))
	case "PUT":
		cidr := strings.TrimPrefix(r.URL.Path, subnetPath)
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

		subnet := s.db.Subnet(net, true)
		if subnet == nil {
			http.Error(w, "Subnet %s does not exist", 404)
			return
		}
		subnet.Name = req.Name
		subnet.Attrs = req.Attrs

		if err := s.db.Save(); err != nil {
			http.Error(w, "Couldn't save change", 500)
			return
		}

		writeJSON(w, subnet)
	case "DELETE":
		cidr := strings.TrimPrefix(r.URL.Path, subnetPath)
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

		subnet := s.db.Subnet(net, true)
		if subnet == nil {
			http.Error(w, "Subnet %s does not exist", 404)
			return
		}

		subnet.Delete(!reparent)

		if err := s.db.Save(); err != nil {
			http.Error(w, "Couldn't save change", 500)
			return
		}
	}
}

func (s *server) Host(w http.ResponseWriter, r *http.Request) {
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
			host := s.db.Host(ip)
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
		if err := s.db.Save(); err != nil {
			http.Error(w, "Couldn't save change", 500)
			return
		}

		writeJSON(w, s.db.Host(req.Addrs[0]))
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

		host := s.db.Host(ip)
		if host == nil {
			http.Error(w, fmt.Sprintf("Host with IP %s does not exist", ip), 404)
			return
		}

		// TODO: needs atomicity here, i.e. a way to roll back the partial edit.
		host.Delete()
		if err := s.db.AddHost(req.Name, req.Addrs, req.Attrs); err != nil {
			http.Error(w, fmt.Sprintf("Editing host %s failed: %s", host.Name, err), 500)
		}
		if err := s.db.Save(); err != nil {
			http.Error(w, "Couldn't save change", 500)
			return
		}

		writeJSON(w, host)
	case "DELETE":
		addr := strings.TrimPrefix(r.URL.Path, hostPath)
		if addr == "" {
			http.Error(w, "Can only DELETE to delete a specific host by IP", 400)
			return
		}
		ip := net.ParseIP(addr)
		if ip == nil {
			http.Error(w, "Malformed IP address", 400)
			return
		}

		host := s.db.Host(ip)
		if host == nil {
			http.Error(w, fmt.Sprintf("Host with IP %s does not exist", ip), 404)
			return
		}

		host.Delete()

		if err := s.db.Save(); err != nil {
			http.Error(w, "Couldn't save change", 500)
			return
		}
	}
}

func (s *server) Bind9(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimPrefix(r.URL.Path, bind9Path)
	zone, err := bind9.ExportZone(s.db, domain, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error exporting zone: %s", err), 500)
		return
	}
	w.Write([]byte(zone))
}
