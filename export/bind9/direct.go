package bind9

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/danderson/gipam/database"
)

func exportDirect(db *database.DB, domain *database.Domain) (string, error) {
	suffix := "." + domain.Name

	ret := []string{
		fmt.Sprintf("$ORIGIN %s.", domain.Name),
		"$TTL 600",
		domain.SOA(),
		"",
	}

	for _, ns := range domain.NS {
		ret = append(ret, fmt.Sprintf("@ IN NS %s.", ns))
	}
	if len(domain.NS) > 0 {
		ret = append(ret, "")
	}
	for _, rr := range domain.RR {
		ret = append(ret, rr)
	}
	if len(domain.RR) > 0 {
		ret = append(ret, "")
	}

	for _, host := range db.Hosts {
		var hostname string
		fqdn := host.Attrs["fqdn"]
		if fqdn != "" {
			if !strings.HasSuffix(fqdn, suffix) {
				continue
			}
			hostname = strings.TrimSuffix(fqdn, suffix)
			for _, addr := range sortedAddrs(host) {
				ret = append(ret, fmt.Sprintf("%s IN %s %s", hostname, rrtype(addr), addr))
			}
		} else {
			hostname = host.Attrs["hostname"]
			if hostname == "" {
				continue
			}
			for _, addr := range sortedAddrs(host) {
				addrDomain := ipDomain(host, addr)
				if addrDomain == "" || addrDomain != domain.Name {
					continue
				}
				ret = append(ret, fmt.Sprintf("%s IN %s %s", hostname, rrtype(addr), addr))
			}
		}

		if cnames := host.Attrs["cname"]; cnames != "" {
			for _, cname := range strings.Split(cnames, ",") {
				ret = append(ret, fmt.Sprintf("%s IN CNAME %s", cname, hostname))
			}
		}
	}

	for _, subnet := range db.Subnets {
		ret = append(ret, walkDirect(db, subnet, domain)...)
	}

	return strings.Join(ret, "\n"), nil
}

func walkDirect(db *database.DB, subnet *database.Subnet, domain *database.Domain) []string {
	var ret []string
	if subnet.Net.IP.To4() == nil {
		// No autogen for ipv6 for now. TODO.
		return nil
	}

	ones, bits := subnet.Net.Mask.Size()
	zeros := uint(bits - ones)
	pattern := subnet.Attrs["dns-autogen-pattern"]
	if pattern == "" || subnetDomain(subnet) != domain.Name || zeros > 8 {
		// No autogen for this subnet, pass through to children.
		for _, s := range subnet.Subnets {
			ret = append(ret, walkDirect(db, s, domain)...)
		}
		return ret
	}

	ret = []string{""}
	ip := make(net.IP, len(subnet.Net.IP))
	copy(ip, subnet.Net.IP)

	lastAddr := int(ip[3] | ((1 << zeros) - 1))

	for i := int(ip[3]); i <= lastAddr; i++ {
		ip[3] = byte(i)
		if db.Host(ip) == nil {
			hostname := strings.Replace(pattern, "$", strconv.Itoa(int(ip[3])), -1)
			ret = append(ret, fmt.Sprintf("%s IN A %s", hostname, ip))
		}
	}
	return ret
}

func nextIP(ip net.IP) {
	n := len(ip) - 1
	for ; n >= 0 && ip[n] == 255; n-- {
		ip[n] = 0
	}
	ip[n]++
}

func rrtype(ip net.IP) string {
	if ip.To4() != nil {
		return "A"
	}
	return "AAAA"
}
