package bind9

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/danderson/gipam/database"
)

func ExportZone(db *database.DB, name string) (string, error) {
	exporter := exportDirect
	if _, _, err := net.ParseCIDR(name); err == nil {
		exporter = exportReverse
	}

	domain, ok := db.Domains[name]
	if !ok {
		return "", fmt.Errorf("Domain %s not found in database", name)
	}

	zone, err := exporter(db, domain)
	if err != nil {
		return "", err
	}

	if zoneHash(zone) != domain.LastHash {
		domain.Serial.Inc()
		zone, err = exporter(db, domain)
		if err != nil {
			return "", err
		}
		domain.LastHash = zoneHash(zone)
	}

	return zone, nil
}

func exportDirect(db *database.DB, domain *database.Domain) (string, error) {
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

func exportReverse(db *database.DB, domain *database.Domain) (string, error) {
	_, net, err := net.ParseCIDR(domain.Name())
	if err != nil {
		panic("Export reverse on a non-CIDR")
	}
	if ones, _ := net.Mask.Size(); ones%8 != 0 {
		return "", fmt.Errorf("Reverse zone CIDR must be 8-bit aligned, cannot generate zone for %s", net)
	}

	ret := []string{
		fmt.Sprintf("$ORIGIN %s", arpaZone(net)),
		domain.SOA(),
		"",
	}

	alloc := db.FindAllocation(&database.IPNet{net}, false)
	if alloc != nil {
		ret = append(ret, walkReverse(net, alloc)...)
	}

	return strings.Join(ret, "\n"), nil
}

func walkReverse(zone *net.IPNet, alloc *database.Allocation) []string {
	var ret []string
	for ipStr, host := range alloc.Hosts() {
		ip := net.ParseIP(ipStr)

		fqdn := host.Attr("fqdn", "")
		if fqdn != "" {
			ret = append(ret, fmt.Sprintf("%s IN PTR %s.", arpaHost(zone, ip), fqdn))
			continue
		}

		hostname := host.Attr("hostname", "")
		if hostname == "" {
			continue
		}
		domain := allocDomain(alloc)
		if domain == "" {
			continue
		}
		ret = append(ret, fmt.Sprintf("%s IN PTR %s.%s.", arpaHost(zone, ip), hostname, domain))
	}

	for _, child := range alloc.Children {
		ret = append(ret, walkReverse(zone, child)...)
	}

	return ret
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

func zoneHash(zone string) string {
	sha := sha1.Sum([]byte(zone))
	return base64.StdEncoding.EncodeToString(sha[:])
}

func rrtype(ip net.IP) string {
	if ip.To4() != nil {
		return "A"
	}
	return "AAAA"
}

func ipDomain(host *database.Host, ip net.IP) string {
	domain := host.Attr("domain", "")
	if domain != "" {
		return domain
	}

	return allocDomain(host.Parent(ip))
}

func allocDomain(alloc *database.Allocation) string {
	for alloc != nil {
		if domain := alloc.Attr("domain", ""); domain != "" {
			return domain
		}
		alloc = alloc.Parent()
	}
	return ""
}
