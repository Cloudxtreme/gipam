package bind9

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/danderson/gipam/database"
)

func exportReverse(db *database.DB, domain *database.Domain) (string, error) {
	_, net, err := net.ParseCIDR(domain.Name)
	if err != nil {
		panic("Export reverse on a non-CIDR")
	}
	if ones, _ := net.Mask.Size(); ones%8 != 0 {
		return "", fmt.Errorf("Reverse zone CIDR must be 8-bit aligned, cannot generate zone for %s", net)
	}

	ret := []string{
		fmt.Sprintf("$ORIGIN %s", arpaZone(net)),
		"$TTL 600",
		domain.SOA(),
		"",
	}

	for _, host := range db.Hosts {
		for _, addr := range sortedAddrs(host) {
			if !net.Contains(addr) {
				continue
			}

			if fqdn := host.Attrs["fqdn"]; fqdn != "" {
				ret = append(ret, fmt.Sprintf("%s IN PTR %s.", arpaHost(net, addr), fqdn))
			} else if hostname := host.Attrs["hostname"]; hostname != "" {
				if domain := ipDomain(host, addr); domain != "" {
					ret = append(ret, fmt.Sprintf("%s IN PTR %s.%s.", arpaHost(net, addr), hostname, domain))
				}
			}
		}
	}

	return strings.Join(ret, "\n"), nil
}

func arpaHost(net *net.IPNet, host net.IP) string {
	var ret []string

	ones, bits := net.Mask.Size()
	end := ones / 8
	start := end + (bits-ones)/8

	if ip := host.To4(); ip != nil {
		for ; start > end; start-- {
			ret = append(ret, strconv.Itoa(int(ip[start-1])))
		}
	} else {
		for ; start > end; start-- {
			u, l := host[start-1]&0xF0, host[start-1]&0xF
			ret = append(ret, strconv.FormatInt(int64(l), 16), strconv.FormatInt(int64(u), 16))
		}
	}

	return strings.Join(ret, ".")
}

func arpaZone(net *net.IPNet) string {
	var ret []string

	ones, _ := net.Mask.Size()
	n := ones / 8

	if ip := net.IP.To4(); ip != nil {
		for ; n > 0; n-- {
			ret = append(ret, strconv.Itoa(int(ip[n-1])))
		}
		ret = append(ret, "in-addr.arpa.")
	} else {
		for ; n > 0; n-- {
			u, l := (net.IP[n-1]&0xF0)>>4, net.IP[n-1]&0xF
			ret = append(ret, strconv.FormatInt(int64(l), 16), strconv.FormatInt(int64(u), 16))
		}
		ret = append(ret, "ip6.arpa.")
	}

	return strings.Join(ret, ".")
}
