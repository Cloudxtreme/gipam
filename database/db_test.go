package database

import (
	"fmt"
	"math/rand"
	"net"
	"testing"
)

func cidr(in string) *IPNet {
	_, net, err := net.ParseCIDR(in)
	if err != nil {
		panic(err)
	}
	return &IPNet{net}
}

func mkTextTree(allocs []*Allocation, indent string) string {
	var ret string
	for _, a := range allocs {
		ret += fmt.Sprintf("%s%s\n", indent, a.Net)
		ret += mkTextTree(a.Children, indent+"  ")
	}
	return ret
}

func deleteNet(db *DB, net *IPNet) error {
	alloc := db.FindAllocation(net, true)
	if alloc == nil {
		return fmt.Errorf("Expected alloc for %s not in DB", net)
	}
	return db.RemoveAllocation(alloc, true)
}

func TestAllocateDeallocate(t *testing.T) {
	t.Parallel()
	ranges := []string{
		"192.168.144.0/22",
		"192.168.144.0/26",
		"192.168.144.0/28",
		"192.168.144.16/29",
		"192.168.144.32/28",
		"192.168.144.56/29",
		"192.168.144.64/31",
		"192.168.144.66/31",
		"192.168.144.68/31",
		"192.168.144.64/27",
		"192.168.144.70/31",
		"192.168.144.72/31",
		"192.168.144.128/25",
		"192.168.144.128/27",
		"192.168.144.240/28",
	}

	for it := 0; it < 1000; it++ {
		// Golden DB is the above order
		golden := New("")
		for _, r := range ranges {
			if err := golden.AddAllocation(cidr(r).String(), cidr(r), nil); err != nil {
				t.Fatalf("Adding %s to golden DB failed: %s", r, err)
			}
		}

		// Build a new DB with a randomized order
		order := rand.Perm(len(ranges))
		var rangesInOrder []string
		db := New("")
		for _, i := range order {
			rangesInOrder = append(rangesInOrder, ranges[i])
			r := cidr(ranges[i])
			if err := db.AddAllocation(r.String(), r, nil); err != nil {
				t.Fatalf("Adding %s to DB failed: %s", r, err)
			}
		}

		// The DB walk should be equivalent at this point.
		a, b := mkTextTree(db.Allocs, "  "), mkTextTree(golden.Allocs, "  ")
		if a != b {
			t.Errorf("DB differs from golden when constructed in order: %#v", rangesInOrder)
			t.Fatalf("got:\n%swant:\n%s", a, b)
		}

		// Now delete in a random order, pulling from golden and db in
		// lockstep, checking for correctness at every step.
		order = rand.Perm(len(ranges))
		rangesInOrder = nil
		for _, i := range order {
			rangesInOrder = append(rangesInOrder, ranges[i])
			if err := deleteNet(golden, cidr(ranges[i])); err != nil {
				t.Errorf("Deleting %s from golden: %s", ranges[i], err)
				t.Errorf("Delete sequence:%#v", rangesInOrder)
				t.Errorf("Golden DB:\n%s", mkTextTree(golden.Allocs, "  "))
				t.FailNow()
			}
			if err := deleteNet(db, cidr(ranges[i])); err != nil {
				t.Errorf("Deleting %s from db: %s", ranges[i], err)
				t.Errorf("Delete sequence:%#v", rangesInOrder)
				t.Errorf("DB:\n%s", mkTextTree(db.Allocs, "  "))
				t.FailNow()
			}

			c, d := mkTextTree(db.Allocs, "  "), mkTextTree(golden.Allocs, "  ")
			if c != d {
				t.Errorf("DB differs from golden after deleting %#v", rangesInOrder)
				t.Fatalf("started with:\n%sdb after deletions:\n%sgolden after deletions:%s", a, b, c)
			}
		}
	}
}

func TestHosts(t *testing.T) {
	t.Parallel()
	ranges := []string{
		"192.168.144.0/22",
		"192.168.144.0/26",
		"192.168.144.0/28",
		"192.168.144.16/29",
		"192.168.144.32/28",
		"192.168.144.56/29",
		"192.168.144.64/31",
		"192.168.144.66/31",
		"192.168.144.68/31",
		"192.168.144.64/27",
		"192.168.144.70/31",
		"192.168.144.72/31",
		"192.168.144.128/25",
		"192.168.144.128/27",
		"192.168.144.240/28",
	}
	db := New("")
	for _, r := range ranges {
		if err := db.AddAllocation(r, cidr(r), nil); err != nil {
			t.Fatalf("Adding %s to DB: %s", r, err)
		}
	}

	type hostToAllocs struct {
		host, alloc string
	}
	tests := []hostToAllocs{
		{"192.168.144.1", "192.168.144.0/28"},
		{"192.168.144.241", "192.168.144.240/28"},
		{"192.168.144.242", "192.168.144.240/28"},
		{"192.168.145.5", "192.168.144.0/22"},
	}
	for _, tc := range tests {
		if err := db.AddHost(tc.host, []net.IP{net.ParseIP(tc.host)}, nil); err != nil {
			t.Fatalf("Adding %s to DB: %s", tc.host, err)
		}
	}

	for _, tc := range tests {
		h := db.FindHost(net.ParseIP(tc.host))
		if h == nil {
			t.Fatalf("%s added to DB, but not found", h)
		}
		if len(h.Addrs) != 1 || !h.Addrs[0].Equal(net.ParseIP(tc.host)) {
			t.Fatalf("Host %s is missing its address", tc.host)
		}

		a := h.parents[tc.host]
		if a.Net.String() != tc.alloc {
			t.Fatalf("Host %s should be in alloc %s, but is actually in %s", tc.host, tc.alloc, a.Net.String())
		}

		if a.hosts[tc.host] != h {
			t.Fatalf("Alloc %s doesn't have a host pointer to %s", a.Net, tc.host)
		}
	}

	tests = []hostToAllocs{
		{"192.168.144.1", "192.168.144.0/26"},
		{"192.168.144.241", "192.168.144.128/25"},
		{"192.168.145.5", ""},
	}
	for _, tc := range tests {
		h := db.FindHost(net.ParseIP(tc.host))
		if err := db.RemoveAllocation(h.parents[tc.host], true); err != nil {
			t.Fatalf("Couldn't delete alloc %s, parent of host %s", h.parents[tc.host].Net, tc.host)
		}

		if tc.alloc == "" {
			if a := h.parents[tc.host]; a != nil {
				t.Fatalf("IP %s should not have a parent, but is parented to %s", tc.host, a.Net.String())
			}
		} else {
			if h.parents[tc.host].Net.String() != tc.alloc {
				t.Fatalf("Host %s should have reparented to %s, but points to %s", tc.host, tc.alloc, h.parents[tc.host].Net)
			}
		}
	}

	tests = []hostToAllocs{
		{"192.168.144.1", "192.168.144.0/28"},
		{"192.168.144.241", "192.168.144.240/28"},
		{"192.168.145.5", "192.168.144.0/22"},
	}

	for _, tc := range tests {
		if err := db.AddAllocation(tc.alloc, cidr(tc.alloc), nil); err != nil {
			t.Fatalf("Couldn't readd alloc %s: %s", tc.alloc, err)
		}
		h := db.FindHost(net.ParseIP(tc.host))
		a := h.parents[tc.host]
		if a == nil {
			t.Fatalf("Host %s should have reparented to %s, but has no parent", tc.host, tc.alloc)
		} else if a.Net.String() != tc.alloc {
			t.Fatalf("Host %s should have reparented to %s, but points to %s", tc.host, tc.alloc, h.parents[tc.host].Net)
		}
	}
}

func TestLastAddr(t *testing.T) {
	t.Parallel()
	type table struct {
		in, out string
	}
	tests := []table{
		{"192.168.208.0/22", "192.168.211.255"},
		{"192.168.210.42/24", "192.168.210.255"},
		{"192.168.210.42/32", "192.168.210.42"},
	}

	for _, test := range tests {
		net := cidr(test.in)
		actual := net.LastAddr().String()
		if actual != test.out {
			t.Errorf("Last address of %s should be %s, got %s", test.in, test.out, actual)
		}
	}
}

func TestNetContains(t *testing.T) {
	t.Parallel()
	type table struct {
		n1, n2  string
		outcome bool
	}
	tests := []table{
		{"192.168.208.0/22", "192.168.208.0/23", true},
		{"192.168.208.0/22", "192.168.208.0/24", true},
		{"192.168.208.0/22", "192.168.208.0/22", true},
		{"192.168.208.0/22", "192.168.209.0/24", true},
		{"192.168.208.0/23", "192.168.208.0/22", false},
	}

	for _, test := range tests {
		n1 := cidr(test.n1)
		n2 := cidr(test.n2)
		res := n1.ContainsNet(n2)
		if res != test.outcome {
			t.Errorf("netContains(%#v, %#v) = %v, want %v", test.n1, test.n2, res, test.outcome)
		}
	}
}
