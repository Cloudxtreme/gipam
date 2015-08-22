package util

import (
	"net"
	"testing"
)

func cidr(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func TestPrefixContains(t *testing.T) {
	cases := []struct {
		a, b   string
		ab, ba bool
	}{
		{"0.0.0.0/0", "0.0.0.0/0", true, true},
		{"192.168.0.0/16", "192.168.0.0/16", true, true},
		{"192.168.1.0/24", "192.168.1.0/24", true, true},
		{"192.168.2.0/24", "192.168.2.0/24", true, true},
		{"192.168.2.128/25", "192.168.2.128/25", true, true},
		{"192.168.1.1/32", "192.168.1.0/24", false, true},
		{"192.168.1.0/26", "192.168.1.0/24", false, true},
		{"10.0.0.0/8", "0.0.0.0/0", false, true},
		{"192.168.10.1/32", "192.168.0.0/16", false, true},
	}

	for _, c := range cases {
		if res := PrefixContains(cidr(c.a), cidr(c.b)); res != c.ab {
			t.Errorf("PrefixContains(%q, %q) = %v, got %v", c.a, c.b, c.ab, res)
		}
		if res := PrefixContains(cidr(c.b), cidr(c.a)); res != c.ba {
			t.Errorf("PrefixContains(%q, %q) = %v, got %v", c.b, c.a, c.ba, res)
		}
	}
}
