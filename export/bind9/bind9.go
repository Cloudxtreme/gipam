package bind9

import (
	"fmt"
	"net"
	"strings"

	db "github.com/danderson/gipam/database"
)

// Generate RRs for a domain
// TODO: generate SOA and NS entries when DB supports it
func ExportZone(db *db.DB, name string) (string, error) {
	domain, ok := db.Domains[name]
	if !ok {
		return "", fmt.Errorf("Domain %s not found in database", name)
	}
	suffix := "." + name

	ret := []string{domain.SOA()}

	for _, ns := range domain.NS {
		ret = append(ret, fmt.Sprintf("@ IN NS %s.", ns))
	}
	for _, rr := range domain.RR {
		ret = append(ret, rr)
	}

	for _, host := range db.Hosts {
		var hostname string
		fqdn := host.Attr("fqdn", "")
		if fqdn != "" {
			if !strings.HasSuffix(fqdn, suffix) {
				continue
			}
			hostname = strings.TrimSuffix(fqdn, suffix)
			for _, addr := range host.Addrs {
				ret = append(ret, fmt.Sprintf("%s IN %s %s", hostname, rrtype(addr), addr))
			}
		} else {
			hostname = host.Attr("hostname", "")
			if hostname == "" {
				continue
			}
			for _, addr := range host.Addrs {
				addrDomain := ipDomain(host, addr)
				if addrDomain == "" || addrDomain != name {
					continue
				}
				ret = append(ret, fmt.Sprintf("%s IN %s %s", hostname, rrtype(addr), addr))
			}
		}

		if cnames := host.Attr("cname", ""); cnames != "" {
			for _, cname := range strings.Split(cnames, ",") {
				ret = append(ret, fmt.Sprintf("%s IN CNAME %s", cname, hostname))
			}
		}
	}
	return strings.Join(ret, "\n"), nil
}

func rrtype(ip net.IP) string {
	if ip.To4() != nil {
		return "A"
	}
	return "AAAA"
}

func ipDomain(host *db.Host, ip net.IP) string {
	domain := host.Attr("domain", "")
	if domain != "" {
		return domain
	}

	alloc := host.Parent(ip)
	for alloc != nil {
		if domain := alloc.Attr("domain", ""); domain != "" {
			return domain
		}
		alloc = alloc.Parent()
	}
	return ""
}
