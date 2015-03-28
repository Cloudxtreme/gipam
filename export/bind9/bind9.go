package bind9

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net"
	"sort"

	"github.com/danderson/gipam/database"
)

func ExportZone(db *database.DB, name string, force bool) (string, error) {
	domain, ok := db.Domains[name]
	if !ok {
		return "", fmt.Errorf("Domain %s not found in database", name)
	}

	zone, err := export(db, domain)
	if err != nil {
		return "", err
	}

	if !force && zoneHash(zone) == domain.LastHash {
		return zone, nil
	}

	domain.Serial.Inc()
	zone, err = export(db, domain)
	if err != nil {
		return "", err
	}
	domain.LastHash = zoneHash(zone)
	return zone, nil
}

func export(db *database.DB, domain *database.Domain) (string, error) {
	if _, _, err := net.ParseCIDR(domain.Name); err == nil {
		return exportReverse(db, domain)
	}
	return exportDirect(db, domain)
}

func zoneHash(zone string) string {
	sha := sha1.Sum([]byte(zone))
	return base64.StdEncoding.EncodeToString(sha[:])
}

func ipDomain(host *database.Host, ip net.IP) string {
	domain := host.Attrs["domain"]
	if domain != "" {
		return domain
	}

	return subnetDomain(host.Parent(ip))
}

func subnetDomain(subnet *database.Subnet) string {
	for subnet != nil {
		if ret := subnet.Attrs["domain"]; ret != "" {
			return ret
		}
		subnet = subnet.Parent
	}
	return ""
}

func sortedAddrs(host *database.Host) []net.IP {
	var s []string
	for a := range host.Addrs {
		s = append(s, a)
	}
	sort.Strings(s)
	var ret []net.IP
	for _, a := range s {
		ret = append(ret, host.Addrs[a])
	}
	return ret
}
