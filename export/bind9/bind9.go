package bind9

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net"
	"strings"

	db "github.com/danderson/gipam/database"
)

func ExportZone(db *db.DB, name string) (string, error) {
	domain, ok := db.Domains[name]
	if !ok {
		return "", fmt.Errorf("Domain %s not found in database", name)
	}

	zone, err := exportDirect(db, domain)
	if err != nil {
		return "", err
	}

	if zoneHash(zone) != domain.LastHash {
		domain.Serial.Inc()
		zone, err = exportDirect(db, domain)
		if err != nil {
			return "", err
		}
		domain.LastHash = zoneHash(zone)
	}

	return zone, nil
}

func zoneHash(zone string) string {
	sha := sha1.Sum([]byte(zone))
	return base64.StdEncoding.EncodeToString(sha[:])
}

func exportDirect(db *db.DB, domain *db.Domain) (string, error) {
	suffix := "." + domain.Name()

	ret := []string{domain.SOA(), ""}

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
				if addrDomain == "" || addrDomain != domain.Name() {
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
