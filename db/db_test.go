package db

import (
	"math/rand"
	"net"
	"reflect"
	"testing"
)

func TestRealm(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal("Cannot create in-memory DB:", err)
	}

	realms := []struct {
		N, D string
	}{
		{"prod", "The real world"},
		{"staging", "The matrix"},
	}

	for _, r := range realms {
		realm := db.Realm(r.N)
		realm.Description = r.D
		if err = realm.Create(); err != nil {
			t.Fatalf("Failed to create realm %s: %s", r.N, err)
		}

		if err = realm.Create(); err != ErrConflict {
			t.Errorf("Was able to create realm %s twice (err: %s)", r.N, err)
		}
	}

	for _, r := range realms {
		realm := db.Realm(r.N)
		if err = realm.Get(); err != nil {
			t.Fatalf("Querying realm %s: %s", r.N, err)
		}

		if realm.Description != r.D {
			t.Errorf("Description in DB for %s doesn't match original", r.N)
		}
	}

	for _, r := range realms {
		newDesc := r.D + "!!!"
		realm := db.Realm(r.N)
		realm.Description = newDesc
		if err = realm.Save(); err != nil {
			t.Fatalf("Editing realm %s: %s", r.N, err)
		}
		if err = realm.Get(); err != nil {
			t.Fatalf("Querying realm %s after edit: %s", r.N, err)
		}
		if realm.Description != newDesc {
			t.Errorf("Realm edit for %s didn't stick in DB", r.N)
		}
	}

	for _, r := range realms {
		realm := db.Realm(r.N)
		if err = realm.Delete(); err != nil {
			t.Fatalf("Deleting realm %s: %s", r.N, err)
		}
		if err = realm.Get(); err != ErrNotFound {
			t.Errorf("DB isn't returning not found after deleting %s", r.N)
		}
		if err = realm.Delete(); err != nil {
			t.Fatalf("Double-deleting realm %s: %s", r.N, err)
		}
	}
}

func CIDR(s string) *net.IPNet {
	_, ret, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return ret
}

func TestPrefix(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal("Cannot create in-memory DB:", err)
	}

	r := db.Realm("prod")
	if err = r.Create(); err != nil {
		t.Fatalf("Creating realm: %s", err)
	}

	prefixes := []string{
		"0.0.0.0/0",
		"192.168.0.0/16",
		"192.168.0.0/24",
		"192.168.1.0/24",
		"192.168.2.0/24",
		"192.168.2.128/25",
	}

	for _, prefix := range prefixes {
		p := r.Prefix(CIDR(prefix))
		p.Description = prefix
		if err = p.Create(); err != nil {
			t.Fatalf("Failed to create prefix %s: %s", prefix, err)
		}

		if err = p.Create(); err != ErrConflict {
			t.Errorf("Was able to create %s twice (err: %s)", prefix, err)
		}
	}

	for _, prefix := range prefixes {
		p := r.Prefix(CIDR(prefix))
		if err = p.Get(); err != nil {
			t.Fatalf("Querying prefix %s: %s", prefix, err)
		}

		if p.Description != prefix {
			t.Errorf("Description in DB for %s doesn't match original", prefix)
		}
	}

	for _, prefix := range prefixes {
		newDesc := prefix + "!!!"
		p := r.Prefix(CIDR(prefix))
		p.Description = newDesc
		if err = p.Save(); err != nil {
			t.Fatalf("Editing prefix %s: %s", prefix, err)
		}
		if err = p.Get(); err != nil {
			t.Fatalf("Querying prefix %s after edit: %s", prefix, err)
		}
		if p.Description != newDesc {
			t.Errorf("Prefix edit for %s didn't stick in DB", prefix)
		}
	}

	roots, err := r.GetPrefixTree()
	if err != nil {
		t.Fatalf("Getting prefix tree: %s", err)
	}

	type flatTree struct {
		pfx   string
		depth int
	}
	expected := []flatTree{
		{"0.0.0.0/0", 0},
		{"192.168.0.0/16", 1},
		{"192.168.0.0/24", 2},
		{"192.168.1.0/24", 2},
		{"192.168.2.0/24", 2},
		{"192.168.2.128/25", 3},
	}
	var walkTree func([]*PrefixTree, int) []flatTree
	walkTree = func(cs []*PrefixTree, depth int) (ret []flatTree) {
		for _, c := range cs {
			ret = append(ret, flatTree{c.Prefix.Prefix.String(), depth})
			ret = append(ret, walkTree(c.Children, depth+1)...)
		}
		return ret
	}
	if !reflect.DeepEqual(walkTree(roots, 0), expected) {
		t.Errorf("GetPrefixTree() = %v, got %v", walkTree(roots, 0), expected)
	}

	for _, prefix := range prefixes {
		p := r.Prefix(CIDR(prefix))
		if err = p.Delete(); err != nil {
			t.Fatalf("Deleting prefix %s: %s", prefix, err)
		}
		if err = p.Get(); err != ErrNotFound {
			t.Errorf("DB isn't returning not found after deleting %s", prefix)
		}
		if err = p.Delete(); err != nil {
			t.Fatalf("Double-deleting realm %s: %s", prefix, err)
		}
	}
}

func TestLongestMatch(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal("Cannot create in-memory DB:", err)
	}

	r := db.Realm("prod")
	if err = r.Create(); err != nil {
		t.Fatalf("Creating realm: %s", err)
	}

	prefixes := []string{
		"0.0.0.0/0",
		"192.168.0.0/16",
		"192.168.1.0/24",
		"192.168.2.0/24",
		"192.168.2.128/25",
	}

	for _, prefix := range prefixes {
		p := r.Prefix(CIDR(prefix))
		p.Description = prefix
		if err = p.Create(); err != nil {
			t.Fatalf("Failed to create prefix %s: %s", prefix, err)
		}
	}

	for _, prefix := range prefixes {
		p := db.Realm("prod").Prefix(CIDR(prefix))
		match, err := p.GetLongestMatch()
		if err != nil {
			t.Fatalf("LPM lookup for %s failed: %s", prefix, err)
		}
		if match.Prefix.String() != prefix {
			t.Errorf("LPM lookup for %s returned %s, not self", prefix, match.Prefix.String())
		}
	}

	lpm := []struct {
		in, out string
	}{
		{"192.168.1.1/32", "192.168.1.0/24"},
		{"192.168.1.0/26", "192.168.1.0/24"},
		{"10.0.0.0/8", "0.0.0.0/0"},
		{"192.168.10.1/32", "192.168.0.0/16"},
	}

	for _, l := range lpm {
		match, err := db.Realm("prod").Prefix(CIDR(l.in)).GetLongestMatch()
		if err != nil {
			t.Errorf("LPM lookup for %s failed: %s", l.in, err)
		}
		if match.Prefix.String() != l.out {
			t.Errorf("LPM lookup for %s returned %s, want %s", l.in, match.Prefix.String(), l.out)
		}
	}
}

func TestMatches(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal("Cannot create in-memory DB:", err)
	}

	r := db.Realm("prod")
	if err = r.Create(); err != nil {
		t.Fatalf("Creating realm: %s", err)
	}

	prefixes := []string{
		"0.0.0.0/0",
		"192.168.0.0/16",
		"192.168.1.0/24",
		"192.168.2.0/24",
		"192.168.2.128/25",
	}

	for _, prefix := range prefixes {
		p := r.Prefix(CIDR(prefix))
		p.Description = prefix
		if err = p.Create(); err != nil {
			t.Fatalf("Failed to create prefix %s: %s", prefix, err)
		}
	}

	lpm := []struct {
		in  string
		out []string
	}{
		{"192.168.1.1/32", []string{"192.168.1.0/24", "192.168.0.0/16", "0.0.0.0/0"}},
		{"192.168.1.0/26", []string{"192.168.1.0/24", "192.168.0.0/16", "0.0.0.0/0"}},
		{"10.0.0.0/8", []string{"0.0.0.0/0"}},
		{"192.168.10.1/32", []string{"192.168.0.0/16", "0.0.0.0/0"}},
	}

	for _, l := range lpm {
		matches, err := db.Realm("prod").Prefix(CIDR(l.in)).GetMatches()
		if err != nil {
			t.Errorf("lPM lookup for %s failed: %s", l.in, err)
		}
		var actual []string
		for _, match := range matches {
			actual = append(actual, match.Prefix.String())
		}
		if !reflect.DeepEqual(actual, l.out) {
			t.Errorf("LPM lookup for %s returned %v, want %v", l.in, actual, l.out)
		}
	}
}

func benchPrefixTree(pfxcnt int, b *testing.B) {
	db, err := New(":memory:")
	if err != nil {
		b.Fatal("Cannot create in-memory DB:", err)
	}

	r := db.Realm("prod")
	if err = r.Create(); err != nil {
		b.Fatalf("Creating realm: %s", err)
	}

	var actualPrefixes int64
	for _, l := range []int{8, 16, 24, 32} {
		for n := 0; n < pfxcnt; n++ {
			ip := make([]byte, 4)
			for i := 0; i < l/8; i++ {
				ip[i] = byte(rand.Int())
			}
			//fmt.Println(net.IP(ip), net.CIDRMask(l, 32))
			pfx := &net.IPNet{net.IP(ip), net.CIDRMask(l, 32)}
			if err := r.Prefix(pfx).Create(); err == nil {
				actualPrefixes++
			}
		}
		pfxcnt *= 10
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		r.GetPrefixTree()
	}
}

func BenchmarkPrefixTree1(b *testing.B) { benchPrefixTree(1, b) }

// func BenchmarkPrefixTree2(b *testing.B)  { benchPrefixTree(2, b) }
// func BenchmarkPrefixTree5(b *testing.B)  { benchPrefixTree(5, b) }
// func BenchmarkPrefixTree10(b *testing.B) { benchPrefixTree(10, b) }
