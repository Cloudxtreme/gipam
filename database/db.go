package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"sort"
	"strconv"
	"time"
)

type DB struct {
	Path    string             `json:"-"`
	Subnets map[string]*Subnet `json:",omitempty"` // cidr->subnet
	Hosts   []*Host            `json:",omitempty"`
	Domains map[string]*Domain `json:",omitempty"`

	ipToHost map[string]*Host
}

func New() *DB {
	return &DB{
		Subnets:  make(map[string]*Subnet),
		Domains:  make(map[string]*Domain),
		ipToHost: make(map[string]*Host),
	}
}

func Load(path string) (*DB, error) {
	f, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	db, err := LoadBytes(f)
	if err != nil {
		return nil, err
	}
	db.Path = path
	return db, nil
}

func LoadBytes(raw []byte) (*DB, error) {
	ret := New()
	if err := json.Unmarshal(raw, &ret); err != nil {
		return nil, err
	}

	recLinkSubnets(ret, ret.Subnets, nil)

	for _, host := range ret.Hosts {
		for addr := range host.Addrs {
			ret.ipToHost[addr] = host
		}
		if host.Addrs == nil {
			host.Addrs = make(HostAddrs)
		}
		host.db = ret
		if host.Attrs == nil {
			host.Attrs = make(map[string]string)
		}
	}

	for _, dom := range ret.Domains {
		dom.db = ret
	}

	if err := ret.validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %s", err)
	}

	return ret, nil
}

func recLinkSubnets(db *DB, subnets map[string]*Subnet, parent *Subnet) {
	for _, s := range subnets {
		if s.Subnets == nil {
			s.Subnets = make(map[string]*Subnet)
		}
		if s.Attrs == nil {
			s.Attrs = make(map[string]string)
		}
		s.Parent = parent
		s.db = db
		recLinkSubnets(db, s.Subnets, s)
	}
}

func (db *DB) Save() error {
	if db.Path == "" {
		return errors.New("No database path defined")
	}
	f, err := db.Bytes()
	if err != nil {
		return err
	}
	return ioutil.WriteFile(db.Path, f, 0640)
}

func (db *DB) Bytes() ([]byte, error) {
	if err := db.validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(db, "", "  ")
}

func (db *DB) validate() error {
	for name, dom := range db.Domains {
		if name != dom.Name {
			return fmt.Errorf("domain %s has map key %s", dom.Name, name)
		}
		if dom.db != db {
			return fmt.Errorf("domain %s belongs to DB %s, want %s", dom.Name, dom.db, db)
		}
	}

	for _, host := range db.Hosts {
		for _, addr := range host.Addrs {
			h, ok := db.ipToHost[addr.String()]
			if !ok {
				return fmt.Errorf("host %s's address %s missing from lookup table", host.Name, addr)
			}
			if h != host {
				return fmt.Errorf("host %s's address %s points to host %#v in lookup table", host.Name, addr, h)
			}
			if host.db != db {
				return fmt.Errorf("host %s belongs to DB %s, want %s", host.Name, host.db, db)
			}
		}
	}

	return recValidateSubnets(db, db.Subnets, nil)
}

func recValidateSubnets(db *DB, subnets map[string]*Subnet, parent *Subnet) error {
	for k, subnet := range subnets {
		if subnet.Net.String() != k {
			return fmt.Errorf("subnet %s has map key %s", subnet.Net, k)
		}
		if subnet.Parent != parent {
			return fmt.Errorf("subnet %s has parent %s, want %s", subnet.Net, subnet.Parent, parent)
		}
		if subnet.db != db {
			return fmt.Errorf("subnet %s belongs to DB %s, want %s", subnet.Net, subnet.db, db)
		}
		if err := recValidateSubnets(db, subnet.Subnets, subnet); err != nil {
			return err
		}
		// TODO: check for bad siblings (that should be children of another sibling)
	}
	return nil
}

// Lookup funcs

func (db *DB) Subnet(net *net.IPNet, exact bool) *Subnet {
	n := (*IPNet)(net)
	for _, subnet := range db.Subnets {
		if ret := subnet.findSubnet(n); ret != nil {
			if exact && !(*IPNet)(ret.Net).Equal(n) {
				return nil
			}
			return ret
		}
	}
	return nil
}

func (db *DB) Host(ip net.IP) *Host {
	if h, ok := db.ipToHost[ip.String()]; ok {
		return h
	}
	return nil
}

func (db *DB) Domain(name string) *Domain {
	if dom, ok := db.Domains[name]; ok {
		return dom
	}
	return nil
}

// Adders

func (db *DB) AddSubnet(name string, net *net.IPNet, attrs map[string]string) error {
	if o, b := net.Mask.Size(); o == b {
		return fmt.Errorf("Cannot allocate %s as a subnet, because it's a host address", net)
	}
	sub := &Subnet{
		Net:     (*IPNet)(net),
		Name:    name,
		Attrs:   attrs,
		Subnets: make(map[string]*Subnet),
		db:      db,
	}

	sub.Parent = db.Subnet(net, false)
	if sub.Parent != nil && sub.Parent.Net.Equal(sub.Net) {
		return fmt.Errorf("Subnet %s already allocated", net)
	}

	m := db.Subnets
	if sub.Parent != nil {
		m = sub.Parent.Subnets
	}
	for k, subnet := range m {
		if sub.Net.ContainsIPNet(subnet.Net) {
			sub.Subnets[k] = subnet
			subnet.Parent = sub
			delete(m, k)
		}
	}
	m[net.String()] = sub

	return nil
}

func (db *DB) AddHost(name string, addrs []net.IP, attrs map[string]string) error {
	for _, addr := range addrs {
		if h, ok := db.ipToHost[addr.String()]; ok {
			return fmt.Errorf("Address %s already in use by host %s", addr, h.Name)
		}
	}

	h := &Host{
		Name:  name,
		Addrs: make(HostAddrs),
		Attrs: attrs,
		db:    db,
	}

	db.Hosts = append(db.Hosts, h)
	for _, addr := range addrs {
		h.AddAddress(addr)
	}

	return nil
}

func (db *DB) AddDomain(name, ns, email string, refresh, retry, expiry, nxttl time.Duration) error {
	// TODO: canonicalize domain name, here we're trusting the user to
	// input the right thing.

	if _, ok := db.Domains[name]; ok {
		return fmt.Errorf("Domain %s already exists in the database", name)
	}

	if _, _, err := net.ParseCIDR(name); err == nil {
		if ns == "" {
			return fmt.Errorf("Must explicitly specify the primary NS for ARPA domain %s", name)
		}
		if email == "" {
			return fmt.Errorf("Must explicitly specify the email for ARPA domain %s", name)
		}
	}

	if ns == "" {
		ns = "ns1." + name
	}
	if email == "" {
		email = "hostmaster." + name
	}
	if refresh == 0 {
		refresh = time.Hour
	}
	if retry == 0 {
		retry = 15 * time.Minute
	}
	if expiry == 0 {
		expiry = 21 * 24 * time.Hour // 3 weeks
	}
	if nxttl == 0 {
		nxttl = 10 * time.Minute
	}

	dom := &Domain{
		Name:         name,
		PrimaryNS:    ns,
		Email:        email,
		SlaveRefresh: refresh,
		SlaveRetry:   retry,
		SlaveExpiry:  expiry,
		NXDomainTTL:  nxttl,

		db: db,
	}

	db.Domains[name] = dom
	return nil
}

// Major datatypes

type Subnet struct {
	Name  string            `json:",omitempty"`
	Attrs map[string]string `json:",omitempty"`

	// Consider these read-only.
	Net     *IPNet
	Subnets map[string]*Subnet `json:",omitempty"` // cidr->Subnet
	Parent  *Subnet            `json:"-"`

	db *DB
}

func (s *Subnet) Delete(recursive bool) {
	m := s.db.Subnets
	if s.Parent != nil {
		m = s.Parent.Subnets
	}
	delete(m, s.Net.String())

	if !recursive {
		for k, subnet := range s.Subnets {
			m[k] = subnet
			subnet.Parent = s.Parent
		}
	}
}

func (s *Subnet) findSubnet(n *IPNet) *Subnet {
	if !s.Net.ContainsIPNet(n) {
		return nil
	}

	for _, child := range s.Subnets {
		if ret := child.findSubnet(n); ret != nil {
			return ret
		}
	}

	return s
}

type Host struct {
	Addrs HostAddrs
	Name  string            `json:",omitempty"`
	Attrs map[string]string `json:",omitempty"`

	db *DB
}

func (h *Host) Delete() {
	for _, addr := range h.Addrs {
		delete(h.db.ipToHost, addr.String())
	}
	var newHosts []*Host
	for _, host := range h.db.Hosts {
		if host != h {
			newHosts = append(newHosts, host)
		}
	}
	h.db.Hosts = newHosts
}

func (h *Host) AddAddress(addr net.IP) error {
	if h, ok := h.db.ipToHost[addr.String()]; ok {
		return fmt.Errorf("address %s already allocated to %s", addr, h.Name)
	}
	h.Addrs[addr.String()] = addr
	h.db.ipToHost[addr.String()] = h
	return nil
}

func (h *Host) RemoveAddress(addr net.IP) error {
	if _, ok := h.Addrs[addr.String()]; !ok {
		return fmt.Errorf("address %s does not belong to %s", addr, h)
	}
	delete(h.Addrs, addr.String())
	delete(h.db.ipToHost, addr.String())
	return nil
}

func (h *Host) Parent(ip net.IP) *Subnet {
	if _, ok := h.Addrs[ip.String()]; !ok {
		return nil
	}

	maskLen := 128
	if isv4(ip) {
		maskLen = 32
	}
	return h.db.Subnet(&net.IPNet{ip, net.CIDRMask(maskLen, maskLen)}, false)
}

type Domain struct {
	Name string

	// SOA parts
	PrimaryNS    string
	Email        string
	SlaveRefresh time.Duration
	SlaveRetry   time.Duration
	SlaveExpiry  time.Duration
	NXDomainTTL  time.Duration

	Serial   ZoneSerial
	LastHash string // SHA1 of the last zone export.

	NS []string `json:",omitempty"`
	RR []string `json:",omitempty"`

	db *DB
}

func (d *Domain) Delete() {
	delete(d.db.Domains, d.Name)
}

func (d *Domain) SOA() string {
	return fmt.Sprintf("@ IN SOA %s. %s. ( %s %d %d %d %d )", d.PrimaryNS, d.Email, d.Serial, int(d.SlaveRefresh.Seconds()), int(d.SlaveRetry.Seconds()), int(d.SlaveExpiry.Seconds()), int(d.NXDomainTTL.Seconds()))
}

// Misc. types

type IPNet net.IPNet

func (n *IPNet) String() string {
	return (*net.IPNet)(n).String()
}

func (n *IPNet) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", n)), nil
}

func (n *IPNet) UnmarshalJSON(b []byte) error {
	_, net, err := net.ParseCIDR(string(b[1 : len(b)-1]))
	if err != nil {
		return err
	}
	*n = IPNet(*net)
	return nil
}

func (n *IPNet) Equal(n2 *IPNet) bool {
	return (*net.IPNet)(n).String() == (*net.IPNet)(n2).String()
}

func (n *IPNet) Contains(ip net.IP) bool {
	return ip.Mask(n.Mask).Equal(n.IP)
}

func (n *IPNet) ContainsIPNet(n2 *IPNet) bool {
	if isv4(n.IP) != isv4(n2.IP) {
		return false
	}

	m1, _ := n.Mask.Size()
	m2, _ := n2.Mask.Size()
	return m2 >= m1 && n.IP.Mask(n.Mask).Equal(n2.IP.Mask(n.Mask))
}

type HostAddrs map[string]net.IP

func (h HostAddrs) MarshalJSON() ([]byte, error) {
	var sorted []string
	for k := range h {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var out []net.IP
	for _, k := range sorted {
		out = append(out, h[k])
	}
	return json.Marshal(out)
}

func (h *HostAddrs) UnmarshalJSON(b []byte) error {
	var in []net.IP
	if err := json.Unmarshal(b, &in); err != nil {
		return err
	}
	*h = make(HostAddrs)
	for _, ip := range in {
		(*h)[ip.String()] = ip
	}
	return nil
}

type ZoneSerial struct {
	date time.Time
	inc  int
}

func (z *ZoneSerial) Inc() {
	now := time.Now().UTC().Truncate(24 * time.Hour)
	y, m, d := z.date.Date()
	y2, m2, d2 := now.Date()
	if y == y2 && m == m2 && d == d2 {
		if z.inc == 99 {
			panic("Zone serial overflow")
		}
		z.inc++
	} else {
		z.date = now
		z.inc = 0
	}
}

func (z ZoneSerial) Before(oz ZoneSerial) bool {
	if z.date.Before(oz.date) {
		return true
	}
	return z.inc < oz.inc
}

func (z ZoneSerial) String() string {
	return fmt.Sprintf("%s%02d", z.date.Format("20060102"), z.inc)
}

func (z *ZoneSerial) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", z.String())), nil
}

func (z *ZoneSerial) UnmarshalJSON(b []byte) error {
	if string(b) == "\"0\"" {
		z.date = time.Time{}
		z.inc = 0
		return nil
	}
	if len(b) != 12 {
		return fmt.Errorf("Invalid zone serial %s", b[1:len(b)-1])
	}
	date, err := time.Parse("20060102", string(b[1:9]))
	if err != nil {
		return fmt.Errorf("Invalid date section of zone serial %s", b[1:len(b)-1])
	}
	inc, err := strconv.Atoi(string(b[9:11]))
	if err != nil {
		return fmt.Errorf("Invalid counter section of zone serial %s", b[1:len(b)-1])
	}
	z.date = date
	z.inc = inc
	return nil
}

func isv4(ip net.IP) bool {
	return ip.To4() != nil
}
