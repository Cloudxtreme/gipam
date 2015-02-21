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
	alloc := db.FindExact(net)
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
		golden := &DB{}
		for _, r := range ranges {
			if err := golden.AddAllocation(cidr(r).String(), cidr(r), nil); err != nil {
				t.Fatalf("Adding %s to golden DB failed: %s", r, err)
			}
		}

		// Build a new DB with a randomized order
		order := rand.Perm(len(ranges))
		var rangesInOrder []string
		db := &DB{}
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
				t.Fatalf("Deleting %s from golden: %s", ranges[i], err)
			}
			if err := deleteNet(db, cidr(ranges[i])); err != nil {
				t.Fatalf("Deleting %s from db: %s", ranges[i], err)
			}

			c, d := mkTextTree(db.Allocs, "  "), mkTextTree(golden.Allocs, "  ")
			if c != d {
				t.Errorf("DB differs from golden after deleting %#v", rangesInOrder)
				t.Fatalf("started with:\n%sdb after deletions:\n%sgolden after deletions:%s", a, b, c)
			}
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
