package main

import (
	"net"
	"testing"
)

func TestIPNetContains(t *testing.T) {
	ip, n, _ := net.ParseCIDR("192.168.100.1/16")
}
