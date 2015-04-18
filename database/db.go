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

// DB is a subnet, host and domain name database.
type DB struct {
	Path string `json:"-"`

	// Treat the following as read-only fields.
	Subnets map[string]*Subnet `json:",omitempty"` // cidr->subnet
	Hosts   []*Host            `json:",omitempty"`
	Domains map[string]*Domain `json:",omitempty"`

	ipToHost map[string]*Host
}

// New returns an empty DB.
func New() *DB {
	return &DB{
		Subnets:  make(map[string]*Subnet),
		Domains:  make(map[string]*Domain),
		ipToHost: make(map[string]*Host),
	}
}

// Load reads a DB from a file.
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

// LoadBytes reads a DB in the form returned by Bytes().
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

// Save writes the DB to db.Path.
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

// Bytes serializes the DB to raw bytes, loadable by LoadBytes.
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

// Subnet returns the allocated Subnet matching the given net, or nil
// if none exists. If exact is false, the search is widened to the
// smallest Subnet that wholly contains net.
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

// Host returns the Host that owns the given IP address, or nil if no
// such host exists.
func (db *DB) Host(ip net.IP) *Host {
	if h, ok := db.ipToHost[ip.String()]; ok {
		return h
	}
	return nil
}

// Domain returns the Domain matching the given name, or nil if no
// such domain exists.
func (db *DB) Domain(name string) *Domain {
	if dom, ok := db.Domains[name]; ok {
		return dom
	}
	return nil
}

// Adders

// AddSubnet allocates a new Subnet with the given settings.
//
// The net must contain at least 2 addresses (i.e. /31 for IPv4, /127
// for IPv6).
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

// AddHost allocates a new Host with the given settings.
//
// Host IPs are globally unique within the DB, no duplicates are
// permitted.
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

// AddDomain allocates a new Domain with the given settings.
//
// For a normal (forward lookup) zone, all attributes except for the
// name are optional, and will default to reasonable values. For an
// ARPA zone (reverse lookup), name, ns and email must all be provided
// because no reasonable defaults exist.
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

// Subnet represents one CIDR block.
type Subnet struct {
	Name  string            `json:",omitempty"`
	Attrs map[string]string `json:",omitempty"`

	// Treat the following as read-only fields.
	Net     *IPNet
	Subnets map[string]*Subnet `json:",omitempty"` // cidr->Subnet
	Parent  *Subnet            `json:"-"`

	db *DB
}

// Delete removes the subnet from the database. If recursive is true,
// children are also deleted instead of being reparented.
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

// Host represents a network host.
type Host struct {
	Addrs HostAddrs
	Name  string            `json:",omitempty"`
	Attrs map[string]string `json:",omitempty"`

	db *DB
}

// Delete removes the host from the database.
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

// AddAddress assigns a new address to the host.
//
// Just like with DB.AddHost, host addresses are glboally unique in
// the DB, no duplication is allowed.
func (h *Host) AddAddress(addr net.IP) error {
	if h, ok := h.db.ipToHost[addr.String()]; ok {
		return fmt.Errorf("address %s already allocated to %s", addr, h.Name)
	}
	h.Addrs[addr.String()] = addr
	h.db.ipToHost[addr.String()] = h
	return nil
}

// RemoveAddress removes an address from the host.
func (h *Host) RemoveAddress(addr net.IP) error {
	if _, ok := h.Addrs[addr.String()]; !ok {
		return fmt.Errorf("address %s does not belong to %s", addr, h)
	}
	delete(h.Addrs, addr.String())
	delete(h.db.ipToHost, addr.String())
	return nil
}

// Parent returns the Subnet containing the given host address.
//
// Returns nil if ip does not belong to the host, or the IP is not
// contained by any allocated subnet.
func (h *Host) Parent(ip net.IP) *Subnet {
	if _, ok := h.Addrs[ip.String()]; !ok {
		return nil
	}

	maskLen := 128
	if isv4(ip) {
		maskLen = 32
	}
	return h.db.Subnet(&net.IPNet{
		IP:   ip,
		Mask: net.CIDRMask(maskLen, maskLen),
	}, false)
}

// Domain records the metadata about a DNS domain.
//
// This is the information found in the Start Of Authority (SOA)
// record for the domain.
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

// Delete removes the domain from the database.
func (d *Domain) Delete() {
	delete(d.db.Domains, d.Name)
}

// SOA returns the SOA record for the domain, in bind9 zonefile format.
func (d *Domain) SOA() string {
	return fmt.Sprintf("@ IN SOA %s. %s. ( %s %d %d %d %d )", d.PrimaryNS, d.Email, d.Serial, int(d.SlaveRefresh.Seconds()), int(d.SlaveRetry.Seconds()), int(d.SlaveExpiry.Seconds()), int(d.NXDomainTTL.Seconds()))
}

// Misc. types

// IPNet wraps net.IPNet for better JSON marshalling.
type IPNet net.IPNet

// String returns the CIDR notation of n like "192.168.100.1/24" or
// "2001:DB8::/48" as defined in RFC 4632 and RFC 4291. If the mask is
// not in the canonical form, it returns the string which consists of
// an IP address, followed by a slash character and a mask expressed
// as hexadecimal form with no punctuation like
// "192.168.100.1/c000ff00".
func (n *IPNet) String() string {
	return (*net.IPNet)(n).String()
}

// MarshalJSON implements the encoding/json.Marshaller interface. The
// encoding is the quoted form of String().
func (n *IPNet) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", n)), nil
}

// UnmarshalJSON implements the encoding/json.Unmarshaller interface.
func (n *IPNet) UnmarshalJSON(b []byte) error {
	_, net, err := net.ParseCIDR(string(b[1 : len(b)-1]))
	if err != nil {
		return err
	}
	*n = IPNet(*net)
	return nil
}

// Equal returns true if n and n2 are the same CIDR block.
func (n *IPNet) Equal(n2 *IPNet) bool {
	return (*net.IPNet)(n).String() == (*net.IPNet)(n2).String()
}

// Contains reports whether the network includes ip.
func (n *IPNet) Contains(ip net.IP) bool {
	return ip.Mask(n.Mask).Equal(n.IP)
}

// ContainsIPNet reports whether n contains n2. n.Equal(n2) implies
// n.ContainsIPNet(n2).
func (n *IPNet) ContainsIPNet(n2 *IPNet) bool {
	if isv4(n.IP) != isv4(n2.IP) {
		return false
	}

	m1, _ := n.Mask.Size()
	m2, _ := n2.Mask.Size()
	return m2 >= m1 && n.IP.Mask(n.Mask).Equal(n2.IP.Mask(n.Mask))
}

// HostAddrs is a set of IP addresses, indexed by their String()
// representation.
type HostAddrs map[string]net.IP

// MarshalJSON implements the encoding/json.Marhsaller
// interface. HostAddrs are encoded as sorted list of IP addresses in
// String() form.
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

// UnmarshalJSON implements the encoding/json.Unmarshaller interface.
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

// ZoneSerial describes a DNS zone serial number, in the de-facto
// standard YYYYMMDDxx format, representing a specific day and an
// incrementing 2-digit counter for same-day zone changes.
type ZoneSerial struct {
	date time.Time
	inc  int
}

// Inc increments z, following the date-as-zone conventions. For
// example, 2014042915 might increment to 2014042916 or 2014043001.
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

// Before returns true if z describes an older zone than oz.
func (z ZoneSerial) Before(oz ZoneSerial) bool {
	if z.date.Before(oz.date) {
		return true
	}
	return z.inc < oz.inc
}

// String returns the zone serial in the YYYYMMDDxx format.
func (z ZoneSerial) String() string {
	return fmt.Sprintf("%s%02d", z.date.Format("20060102"), z.inc)
}

// MarshalJSON implements the encoding/json.Marhaller interface. The
// encoding is the same as String().
func (z *ZoneSerial) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", z.String())), nil
}

// UnmarshalJSON implements the encoding/json.Unmarshaller interface.
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
