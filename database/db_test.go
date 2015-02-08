package database

import (
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

func TestLastAddr(t *testing.T) {
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
		actual := lastAddr(net).String()
		if actual != test.out {
			t.Errorf("Last address of %s should be %s, got %s", test.in, test.out, actual)
		}
	}
}

func TestNetContains(t *testing.T) {
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
		res := netContains(n1, n2)
		if res != test.outcome {
			t.Errorf("netContains(%#v, %#v) = %v, want %v", test.n1, test.n2, res, test.outcome)
		}
	}
}
