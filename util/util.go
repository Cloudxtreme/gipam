package util

import (
	"fmt"
	"net"
)

func PrefixContains(n1, n2 *net.IPNet) bool {
	if isv4(n1.IP) != isv4(n2.IP) {
		return false
	}

	m1, s1 := n1.Mask.Size()
	m2, s2 := n2.Mask.Size()
	if s1 == 0 {
		panic(fmt.Sprintf("%q is not a well-formed CIDR prefix", n1))
	} else if s2 == 0 {
		panic(fmt.Sprintf("%q is not a well-formed CIDR prefix", n2))
	}

	return m2 >= m1 && n1.IP.Mask(n1.Mask).Equal(n2.IP.Mask(n1.Mask))
}

func isv4(n net.IP) bool {
	return n.To4() != nil
}
