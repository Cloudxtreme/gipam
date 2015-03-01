package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

type DB struct {
	Name    string
	Allocs  []*Allocation
	Hosts   []*Host
	Domains map[string]*Domain

	// Index of address to host
	hostLookup map[string]*Host
}

func New(name string) *DB {
	return &DB{
		Name:       name,
		Allocs:     []*Allocation{},
		Hosts:      []*Host{},
		hostLookup: make(map[string]*Host),
	}
}

func Load(path string) (*DB, error) {
	f, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d := New("")
	if err = json.Unmarshal(f, d); err != nil {
		return nil, err
	}
	// TODO: validate
	for _, a := range d.Allocs {
		recursiveSetParent(a, nil)
	}
	for _, h := range d.Hosts {
		if h.Attrs == nil {
			h.Attrs = make(map[string]string)
		}
		h.parents = make(map[string]*Allocation)
		for _, a := range h.Addrs {
			d.hostLookup[a.String()] = h
			alloc := d.FindAllocation(hostToNet(a), false)
			if alloc != nil {
				alloc.hosts[a.String()] = h
			}
			h.parents[a.String()] = alloc
		}
	}
	if d.Domains == nil {
		d.Domains = make(map[string]*Domain)
	}
	for name, dom := range d.Domains {
		dom.name = name
	}
	return d, nil
}

func recursiveSetParent(a *Allocation, p *Allocation) {
	a.parent = p
	a.hosts = make(map[string]*Host)
	if a.Attrs == nil {
		a.Attrs = make(map[string]string)
	}
	for _, c := range a.Children {
		recursiveSetParent(c, a)
	}
}

func (d *DB) Save(path string) error {
	f, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, f, 0640)
}

func (d *DB) FindAllocation(n *IPNet, exact bool) *Allocation {
	for _, a := range d.Allocs {
		if ret := a.findContainer(n); ret != nil {
			if exact && !ret.Net.Equal(n) {
				return nil
			}
			return ret
		}
	}
	return nil
}

func (d *DB) FindHost(addr net.IP) *Host {
	return d.hostLookup[addr.String()]
}

// addChildren adds viable allocations from candidates as children of
// alloc. Mutates alloc.Children and returns the non-viable
// candidates.
func addChildren(alloc *Allocation, candidates []*Allocation) []*Allocation {
	var ret []*Allocation
	for _, a := range candidates {
		if alloc.Net.ContainsNet(a.Net) {
			alloc.Children = append(alloc.Children, a)
			a.parent = alloc
		} else {
			ret = append(ret, a)
		}
	}
	sort.Sort(allocSort(alloc.Children))
	return ret
}

func (d *DB) AddAllocation(name string, network *IPNet, attrs map[string]string) error {
	if ishost(network) {
		return fmt.Errorf("Cannot allocate %s as an allocation, because it's a host address", network)
	}
	alloc := &Allocation{
		Net:   network,
		Name:  name,
		Attrs: attrs,
		hosts: make(map[string]*Host),
	}
	parent := d.FindAllocation(alloc.Net, false)
	if parent == nil {
		d.Allocs = addChildren(alloc, d.Allocs)
		d.Allocs = append(d.Allocs, alloc)
		sort.Sort(allocSort(d.Allocs))
		for _, h := range d.Hosts {
			for ipStr, parent := range h.parents {
				if parent != nil {
					continue
				}

				ip := net.ParseIP(ipStr)
				if ip == nil {
					panic("Bad IP found in DB")
				}
				if alloc.Net.Contains(ip) {
					alloc.hosts[ipStr] = h
					h.parents[ipStr] = alloc
				}
			}
		}
		// TODO: more complex host reparenting
	} else if parent.Net.Equal(alloc.Net) {
		return fmt.Errorf("%s already allocated as \"%s\"", parent.Net, parent.Name)
	} else {
		parent.Children = addChildren(alloc, parent.Children)
		parent.Children = append(parent.Children, alloc)
		sort.Sort(allocSort(parent.Children))
		alloc.parent = parent
		for ipStr, host := range parent.hosts {
			ip := net.ParseIP(ipStr)
			if ip == nil {
				panic("Bad IP found in DB")
			}
			if alloc.Net.Contains(ip) {
				delete(parent.hosts, ipStr)
				alloc.hosts[ipStr] = host
				host.parents[ipStr] = alloc
			}
		}
	}
	return nil
}

func removeAlloc(as []*Allocation, a *Allocation) []*Allocation {
	var ret []*Allocation
	for _, b := range as {
		if a != b {
			ret = append(ret, b)
		}
	}
	return ret
}

func (d *DB) RemoveAllocation(a *Allocation, reparentChildren bool) error {
	c := d.FindAllocation(a.Net, true)
	if c == nil {
		return fmt.Errorf("Allocation %s is not part of this DB", a.Net)
	}
	if a.parent == nil {
		d.Allocs = removeAlloc(d.Allocs, c)
		if reparentChildren {
			d.Allocs = append(d.Allocs, c.Children...)
			for _, a := range d.Allocs {
				a.parent = nil
			}
			sort.Sort(allocSort(d.Allocs))
		}
		for ip, host := range c.hosts {
			host.parents[ip] = nil
		}
	} else {
		a.parent.Children = removeAlloc(a.parent.Children, c)
		if reparentChildren {
			a.parent.Children = append(a.parent.Children, c.Children...)
			for _, c := range a.parent.Children {
				c.parent = a.parent
			}
			sort.Sort(allocSort(a.parent.Children))
		}
		for ip, host := range c.hosts {
			host.parents[ip] = a.parent
			a.parent.hosts[ip] = host
		}
	}
	a.Children = nil
	a.parent = nil

	return nil
}

func (d *DB) AddHost(name string, addrs []net.IP, attrs map[string]string) error {
	if len(addrs) == 0 {
		return fmt.Errorf("Hosts must have at least one IP address")
	}
	for _, a := range addrs {
		if h, ok := d.hostLookup[a.String()]; ok {
			return fmt.Errorf("Cannot add host '%s': address %s already belongs to '%s'", name, a, h.Name)
		}
	}

	h := &Host{
		Name:    name,
		Addrs:   addrs,
		Attrs:   attrs,
		parents: make(map[string]*Allocation),
	}

	for _, a := range addrs {
		d.hostLookup[a.String()] = h
		alloc := d.FindAllocation(hostToNet(a), false)
		if alloc != nil {
			alloc.hosts[a.String()] = h
		}
		h.parents[a.String()] = alloc
	}

	d.Hosts = append(d.Hosts, h)
	return nil
}

func (d *DB) RemoveHost(h *Host) error {
	for _, ip := range h.Addrs {
		if d.FindHost(ip) != h {
			return fmt.Errorf("Host with IP %s is not part of this DB", ip)
		}
	}

	for ip, alloc := range h.parents {
		if alloc != nil {
			delete(alloc.hosts, ip)
		}
		delete(d.hostLookup, ip)
	}
	h.parents = nil

	var newHosts []*Host
	for _, host := range d.Hosts {
		if h != host {
			newHosts = append(newHosts, host)
		}
	}
	d.Hosts = newHosts
	return nil
}

func (d *DB) AddHostAddr(h *Host, a net.IP) error {
	other := d.FindHost(a)
	if other != nil {
		return fmt.Errorf("Other host %s already has the address %s", other.Name, a)
	}
	alloc := d.FindAllocation(hostToNet(a), false)
	if alloc != nil {
		alloc.hosts[a.String()] = h
	}
	h.parents[a.String()] = alloc
	h.Addrs = append(h.Addrs, a)
	d.hostLookup[a.String()] = h
	return nil
}

func (d *DB) RmHostAddr(h *Host, a net.IP) error {
	alloc, ok := h.parents[a.String()]
	if !ok {
		return fmt.Errorf("Host %s doesn't have the address %s", h.Name, a)
	}
	if alloc != nil {
		delete(alloc.hosts, a.String())
	}
	delete(h.parents, a.String())
	delete(d.hostLookup, a.String())
	var newAddrs []net.IP
	for _, addr := range h.Addrs {
		if !addr.Equal(a) {
			newAddrs = append(newAddrs, addr)
		}
	}
	h.Addrs = newAddrs

	return nil
}

func (d *DB) AddDomain(name, ns, email string, refresh, retry, expiry, nxttl time.Duration) error {
	// TODO: canonicalize domain name, here we're trusting the user to
	// input the right thing.
	if _, ok := d.Domains[name]; ok {
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
		PrimaryNS:    ns,
		Email:        email,
		SlaveRefresh: refresh,
		SlaveRetry:   retry,
		SlaveExpiry:  expiry,
		NXDomainTTL:  nxttl,
	}

	d.Domains[name] = dom
	return nil
}

func (d *DB) RmDomain(name string) error {
	if _, ok := d.Domains[name]; !ok {
		return fmt.Errorf("Domain %s does not exist in the database", name)
	}
	delete(d.Domains, name)
	return nil
}

type Allocation struct {
	Net      *IPNet
	Name     string            `json:",omitempty"`
	Attrs    map[string]string `json:",omitempty"`
	Children []*Allocation     `json:",omitempty"`

	parent *Allocation
	// Index of IP to host
	hosts map[string]*Host
}

func (a *Allocation) Attr(name, dflt string) string {
	if ret, ok := a.Attrs[name]; ok {
		return ret
	}
	return dflt
}

func (a *Allocation) Parent() *Allocation {
	return a.parent
}

func (a *Allocation) Hosts() map[string]*Host {
	return a.hosts
}

func (a *Allocation) findContainer(n *IPNet) *Allocation {
	if !a.Net.ContainsNet(n) {
		return nil
	}

	for _, c := range a.Children {
		if child := c.findContainer(n); child != nil {
			return child
		}
	}

	return a
}

type Host struct {
	Addrs []net.IP
	Name  string            `json:",omitempty"`
	Attrs map[string]string `json:",omitempty"`

	// Index of IP to its parent allocation
	parents map[string]*Allocation
}

func (h *Host) Attr(name, dflt string) string {
	if ret, ok := h.Attrs[name]; ok {
		return ret
	}
	return dflt
}

func (h *Host) Parent(ip net.IP) *Allocation {
	return h.parents[ip.String()]
}

type Domain struct {
	name string

	// SOA parts
	PrimaryNS    string `json:",omitempty"`
	Email        string `json:",omitempty"`
	SlaveRefresh time.Duration
	SlaveRetry   time.Duration
	SlaveExpiry  time.Duration
	NXDomainTTL  time.Duration

	Serial   ZoneSerial
	LastHash string // SHA1 of the last zone export.

	NS []string `json:",omitempty"`
	RR []string `json:",omitempty"`
}

func (d *Domain) Name() string {
	return d.name
}

func (d *Domain) SOA() string {
	ns := d.PrimaryNS
	if ns == "" {
		ns = fmt.Sprintf("ns1.%s", d.name)
	}
	email := strings.Replace(d.Email, "@", ".", -1)
	if email == "" {
		email = fmt.Sprintf("hostmaster.%s", d.name)
	}
	return fmt.Sprintf("@ IN SOA %s. %s. ( %s %d %d %d %d )", ns, email, d.Serial, int(d.SlaveRefresh.Seconds()), int(d.SlaveRetry.Seconds()), int(d.SlaveExpiry.Seconds()), int(d.NXDomainTTL.Seconds()))
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

type IPNet struct {
	*net.IPNet
}

func (n *IPNet) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", n)), nil
}

func (n *IPNet) UnmarshalJSON(b []byte) error {
	_, net, err := net.ParseCIDR(string(b[1 : len(b)-1]))
	if err != nil {
		return err
	}
	*n = IPNet{net}
	return nil
}

func (n *IPNet) ContainsNet(n2 *IPNet) bool {
	if isv4(n.IP) != isv4(n2.IP) {
		return false
	}

	o1, _ := n.Mask.Size()
	o2, _ := n2.Mask.Size()
	return o2 >= o1 && n.IP.Mask(n.Mask).Equal(n2.IP.Mask(n.Mask))
}

func (n *IPNet) Equal(n2 *IPNet) bool {
	return n.ContainsNet(n2) && n2.ContainsNet(n)
}

func (n *IPNet) Less(n2 *IPNet) bool {
	if isv4(n.IP) != isv4(n2.IP) {
		return isv4(n.IP)
	}

	o1, _ := n.Mask.Size()
	o2, _ := n.Mask.Size()
	if o1 < o2 {
		return true
	}

	i1 := n.IP.Mask(n.Mask).To16()
	i2 := n2.IP.Mask(n2.Mask).To16()
	return bytes.Compare(i1, i2) < 0
}

func (n *IPNet) FirstAddr() net.IP {
	return n.IP.Mask(n.Mask)
}

func (n *IPNet) LastAddr() net.IP {
	ret := n.IP.Mask(n.Mask)
	if isv4(ret) {
		ret = ret.To4()
	}
	ones, bits := n.Mask.Size()
	zeros := bits - ones
	for i := range ret {
		ones -= 8
		if ones < 0 {
			if zeros%8 == 0 {
				ret[i] = 0xff
				zeros -= 8
			} else {
				ret[i] |= (1 << uint(zeros%8)) - 1
				zeros -= zeros % 8
			}
		}
	}
	return ret
}

func isv4(ip net.IP) bool {
	return ip.To4() != nil
}

func ishost(n *IPNet) bool {
	ones, bits := n.Mask.Size()
	return ones == bits
}

func hostToNet(i net.IP) *IPNet {
	n := &IPNet{&net.IPNet{
		IP: i,
	}}
	if isv4(i) {
		n.Mask = net.CIDRMask(32, 32)
	} else {
		n.Mask = net.CIDRMask(128, 128)
	}
	return n
}

// TODO: make this sort by CIDR prefix correctly, rather than just
// textually.
type allocSort []*Allocation

func (a allocSort) Len() int {
	return len(a)
}

func (a allocSort) Less(i, j int) bool {
	return a[i].Net.Less(a[j].Net)
}

func (a allocSort) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}
