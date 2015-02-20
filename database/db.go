package database

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
)

type DB struct {
	Name   string
	Allocs []*Allocation
}

func Load(path string) (*DB, error) {
	f, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d DB
	if err = json.Unmarshal(f, &d); err != nil {
		return nil, err
	}
	// TODO: validate
	for _, a := range d.Allocs {
		a.setParent(nil)
	}
	return &d, nil
}

func Save(path string, d *DB) error {
	f, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, f, 0640)
}

// FindContainer returns the smallest Allocation that contains n.
// Returns nil if no matching allocations exist.
func (d *DB) FindContainer(n *IPNet) *Allocation {
	for _, a := range d.Allocs {
		if ret := a.findContainer(n); ret != nil {
			return ret
		}
	}
	return nil
}

func (d *DB) FindExact(n *IPNet) *Allocation {
	a := d.FindContainer(n)
	if a == nil || !a.Net.Equal(n) {
		return nil
	}
	return a
}

func (d *DB) Allocate(name string, net *IPNet, attrs map[string]string) error {
	alloc := &Allocation{
		Net:   net,
		Name:  name,
		Attrs: attrs,
	}
	parent := d.FindContainer(alloc.Net)
	if parent == nil {
		d.Allocs = append(d.Allocs, alloc)
	} else if parent.Net.Equal(alloc.Net) {
		return fmt.Errorf("%s already allocated as \"%s\"", parent.Net, parent.Name)
	} else {
		newChildren := []*Allocation{alloc}
		// TODO: caca
		for _, a := range parent.Children {
			if alloc.Net.ContainsNet(a.Net) {
				alloc.Children = append(alloc.Children, a)
			} else {
				newChildren = append(newChildren, a)
			}
		}
		alloc.setParent(parent)
		parent.Children = newChildren
	}
	return nil
}

func (d *DB) Deallocate(a *Allocation, reparentChildren bool) error {
	c := d.FindContainer(a.Net)
	if a != c {
		return fmt.Errorf("Allocation %s is not part of this DB", a.Net)
	}
	newChildren := []*Allocation{}
	for _, c = range a.parent.Children {
		if c != a {
			newChildren = append(newChildren, c)
		}
	}
	if reparentChildren {
		for _, c := range a.Children {
			c.setParent(a.parent)
		}
		newChildren = append(newChildren, a.Children...)
	}
	a.parent.Children = newChildren
	a.Children = nil
	a.parent = nil

	return nil
}

type Allocation struct {
	Net      *IPNet
	Name     string            `json:",omitempty"`
	Attrs    map[string]string `json:",omitempty"`
	Children []*Allocation     `json:",omitempty"`
	parent   *Allocation
}

func (a *Allocation) IsHost() bool {
	ones, bits := a.Net.Mask.Size()
	return ones == bits
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

func (a *Allocation) setParent(p *Allocation) {
	a.parent = p
	for _, c := range a.Children {
		c.setParent(a)
	}
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

func (n *IPNet) Family() string {
	return family(n.IP)
}

func (n *IPNet) ContainsNet(n2 *IPNet) bool {
	if n.Family() != n2.Family() {
		return false
	}

	o1, _ := n.Mask.Size()
	o2, _ := n2.Mask.Size()
	return o2 >= o1 && n.IP.Mask(n.Mask).Equal(n2.IP.Mask(n.Mask))

}

func (n *IPNet) Equal(n2 *IPNet) bool {
	return n.ContainsNet(n2) && n2.ContainsNet(n)
}

func (n *IPNet) FirstAddr() net.IP {
	return n.IP.Mask(n.Mask)
}

func (n *IPNet) LastAddr() net.IP {
	ret := n.IP.Mask(n.Mask)
	if family(ret) == "ipv4" {
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

func family(ip net.IP) string {
	if ip.To4() != nil {
		return "ipv4"
	}
	return "ipv6"
}
