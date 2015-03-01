package main

import (
	"fmt"
	"net"
	"os"
	"sort"
	"time"

	"github.com/danderson/gipam/database"
	docopt "github.com/docopt/docopt-go"
)

var (
	usage = `GIPAM Network and Host management.

Usage:
  gipam [-d DB] [--debug] <command> [<args>...]
  gipam (--help | --version)

Options:
  -d DB, --db=DB  Path to the database file [default: db]

Available commands:
  new      Create a new database
  list     List contents of the database
  show     Show detailed data about database objects
  add      Add an object to the database
  rm       Remove an object from the database
  setattr  Set attributes on database objects
  rmattr   Remove attributes on database objects
  server   Run the GIPAM server

See 'gipam help <command>' for more information on a specific command.
`

	subUsage = map[string]string{
		"new":  "Usage: gipam new\n",
		"list": "Usage: gipam list (subnets | hosts | domains)\n",
		"show": `Usage:
  gipam show subnet <cidr>
  gipam show host <addr>
  gipam show domain <domain>
`,
		"add": `Usage:
  gipam add subnet <name> <cidr> [(<key> <value>)...]
  gipam add host <name> <addr> [(<key> <value>)...]
  gipam add address <addr> <addrs>...
  gipam add domain <name> [--primaryns=NS] [--email=EMAIL] [--slave-refresh=REFRESH] [--slave-retry=RETRY] [--slave-expiry=EXPIRY] [--nxdomain-ttl=TTL]

Options:
 --primaryns=NS          Primary NS (SOA record)
 --email=EMAIL           Hostmaster email (SOA record)
 --slave-refresh=REFRESH Zone refresh time (SOA record)
 --slave-retry=RETRY     Slave retry interval (SOA record)
 --slave-expiry=EXPIRY   Zone expiry time (SOA record)
 --nxdomain-ttl=TTL      TTL to return on NXDOMAIN responses
`,
		"rm": `Usage:
  gipam rm subnet <cidr> [--recursive]
  gipam rm host <addr>
  gipam rm address <addr> <addrs>...
  gipam rm domain <domain>

Options:
  -r, --recursive  Delete child subnets instead of reparenting
`,
		"setattr": "Usage: gipam setattr (<cidr> | <addr>) (<key> <value>)...\n",
		"rmattr":  "Usage: gipam rmattr (<cidr> | <addr>) <key>...\n",
		"server":  "Usage: gipam server [--addr=0.0.0.0] <port>\n",
	}
)

func main() {
	args := parse(usage, os.Args[1:], true)
	cmd := args["<command>"].(string)
	subcmd := []string{cmd}
	subcmd = append(subcmd, args["<args>"].([]string)...)
	dbPath := args["--db"].(string)

	switch cmd {
	case "help":
		if len(subcmd) == 2 {
			if u, ok := subUsage[subcmd[1]]; ok {
				os.Stdout.WriteString(u)
			} else {
				fmt.Printf("%s is not a gipam command. See 'gipam help'.\n", subcmd[1])
			}
		} else {
			os.Stdout.WriteString(usage)
		}
	case "new":
		New(dbPath)
	case "list":
		List(getDB(dbPath), subcmd)
	case "show":
		Show(getDB(dbPath), subcmd)
	case "add":
		Add(dbPath, getDB(dbPath), subcmd)
	case "rm":
		Rm(dbPath, getDB(dbPath), subcmd)
	case "setattr":
		SetAttr(dbPath, getDB(dbPath), subcmd)
	case "rmattr":
		RmAttr(dbPath, getDB(dbPath), subcmd)
	case "server":
		Server(dbPath, getDB(dbPath), subcmd)
	}
}

func New(dbPath string) {
	f, err := os.OpenFile(dbPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
	if err != nil {
		fatal("Failed to create database: %s", err)
	}
	if _, err = f.WriteString("{}"); err != nil {
		fatal("Failed to create database: %s", err)
	}
	fmt.Printf("Wrote empty database to %s.\n", dbPath)
}

func List(db *database.DB, argv []string) {
	args := parse(subUsage["list"], argv, false)
	switch {
	case args["subnets"].(bool):
		allocsAsTree(db.Allocs, "")
	case args["hosts"].(bool):
		for _, h := range db.Hosts {
			fmt.Printf("%s\n", h.Name)
			for _, a := range h.Addrs {
				fmt.Printf("  %s\n", a)
			}
		}
	case args["domains"].(bool):
		for name := range db.Domains {
			fmt.Printf("%s\n", name)
		}
	default:
		panic("unreachable")
	}
}

func Show(db *database.DB, argv []string) {
	args := parse(subUsage["show"], argv, false)
	switch {
	case args["subnet"].(bool):
		cidr := cidr(args["<cidr>"].(string))
		alloc := db.FindAllocation(cidr, true)
		if alloc == nil {
			fatal("Subnet %s not found in database", cidr)
		}
		fmt.Printf("Name: %s\nPrefix: %s\n", alloc.Name, alloc.Net)
		for _, k := range sortedAttrKeys(alloc.Attrs) {
			fmt.Printf("%s: %s\n", k, alloc.Attrs[k])
		}
	case args["host"].(bool):
		addr := addr(args["<addr>"].(string))
		host := db.FindHost(addr)
		if host == nil {
			fatal("Host with IP address %s not found in database", addr)
		}
		fmt.Printf("Name: %s\n", host.Name)
		for _, a := range host.Addrs {
			fmt.Printf("Addr: %s\n", a)
		}
		for _, k := range sortedAttrKeys(host.Attrs) {
			fmt.Printf("%s: %s\n", k, host.Attrs[k])
		}
	case args["domain"].(bool):
		name := args["<domain>"].(string)
		domain, ok := db.Domains[name]
		if !ok {
			fatal("Domain %s not found in database", name)
		}
		fmt.Printf("Name: %s\nSOA: %s\n", name, domain.SOA())
	default:
		panic("unreachable")
	}
}

func Add(dbPath string, db *database.DB, argv []string) {
	args := parse(subUsage["add"], argv, false)
	switch {
	case args["subnet"].(bool):
		name := args["<name>"].(string)
		cidr := cidr(args["<cidr>"].(string))
		attrs := attrs(args["<key>"].([]string), args["<value>"].([]string))
		if err := db.AddAllocation(name, cidr, attrs); err != nil {
			fatal("Error creating subnet: %s", err)
		}
		saveDB(dbPath, db)
		fmt.Printf("Added subnet %s\n", cidr)
	case args["host"].(bool):
		name := args["<name>"].(string)
		addr := addr(args["<addr>"].(string))
		attrs := attrs(args["<key>"].([]string), args["<value>"].([]string))
		if err := db.AddHost(name, []net.IP{addr}, attrs); err != nil {
			fatal("Error creating host: %s", err)
		}
		saveDB(dbPath, db)
		fmt.Printf("Added host %s\n", name)
	case args["address"].(bool):
		hostAddr := addr(args["<addr>"].(string))
		host := db.FindHost(hostAddr)
		if host == nil {
			fatal("Host with IP address %s not found in database", hostAddr)
		}
		for _, a := range args["<addrs>"].([]string) {
			addr := addr(a)
			if err := db.AddHostAddr(host, addr); err != nil {
				fatal("Error adding address %s: %s", addr, err)
			}
		}
		saveDB(dbPath, db)
		fmt.Printf("Added address %s to host %s\n", hostAddr, host.Name)
	case args["domain"].(bool):
		name := args["<name>"].(string)
		if _, ok := db.Domains[name]; ok {
			fatal("Domain %s already exists in database", name)
		}
		var (
			ns, email                     string
			refresh, retry, expiry, nxttl time.Duration
		)
		if args["--primaryns"] != nil {
			ns = args["--primaryns"].(string)
		}
		if args["--email"] != nil {
			email = args["--email"].(string)
		}
		if args["--slave-refresh"] != nil {
			refresh = duration(args["--slave-refresh"].(string))
		}
		if args["--slave-retry"] != nil {
			retry = duration(args["--slave-retry"].(string))
		}
		if args["--slave-expiry"] != nil {
			expiry = duration(args["--slave-expiry"].(string))
		}
		if args["--nxdomain-ttl"] != nil {
			nxttl = duration(args["--nxdomain-ttl"].(string))
		}
		if err := db.AddDomain(name, ns, email, refresh, retry, expiry, nxttl); err != nil {
			fatal("Error adding domain: %s", err)
		}
		saveDB(dbPath, db)
		fmt.Printf("Added domain %s\n", name)
	default:
		panic("unreachable")
	}
}

func Rm(dbPath string, db *database.DB, argv []string) {
	args := parse(subUsage["rm"], argv, false)
	switch {
	case args["subnet"].(bool):
		cidr := cidr(args["<cidr>"].(string))
		alloc := db.FindAllocation(cidr, true)
		if alloc == nil {
			fatal("Subnet %s not found in database", cidr)
		}
		if err := db.RemoveAllocation(alloc, !args["--recursive"].(bool)); err != nil {
			fatal("Error removing subnet: %s", err)
		}
		saveDB(dbPath, db)
		fmt.Printf("Deleted subnet %s\n", cidr)
	case args["host"].(bool):
		addr := addr(args["<addr>"].(string))
		host := db.FindHost(addr)
		if host == nil {
			fatal("Host with IP address %s not found in database", addr)
		}
		if err := db.RemoveHost(host); err != nil {
			fatal("Error removing host: %s", err)
		}
		saveDB(dbPath, db)
		fmt.Printf("Deleted host %s\n", host.Name)
	case args["address"].(bool):
		hostAddr := addr(args["<addr>"].(string))
		host := db.FindHost(hostAddr)
		if host == nil {
			fatal("Host with IP address %s not found in database", hostAddr)
		}
		for _, a := range args["<addrs>"].([]string) {
			addr := addr(a)
			if err := db.RmHostAddr(host, addr); err != nil {
				fatal("Error removing address %s: %s", addr, err)
			}
		}
		saveDB(dbPath, db)
		fmt.Printf("Removed address %s from host %s\n", hostAddr, host.Name)
	case args["domain"].(bool):
		name := args["<domain>"].(string)
		if _, ok := db.Domains[name]; !ok {
			fatal("Domain %s does not exist in database", name)
		}
		if err := db.RmDomain(name); err != nil {
			fatal("Error deleting domain: %s", err)
		}
		saveDB(dbPath, db)
		fmt.Printf("Removed domain %s\n", name)
	default:
		panic("unreachable")
	}
}

func SetAttr(dbPath string, db *database.DB, argv []string) {
	args := parse(subUsage["setattr"], argv, false)
	attrs := attrs(args["<key>"].([]string), args["<value>"].([]string))
	selector := args["<cidr>"].(string)
	_, cidr, err := net.ParseCIDR(selector)
	if err == nil {
		alloc := db.FindAllocation(&database.IPNet{cidr}, true)
		if alloc == nil {
			fatal("Subnet %s not found in database", cidr)
		}
		for k, v := range attrs {
			alloc.Attrs[k] = v
		}
		saveDB(dbPath, db)
		fmt.Printf("Edited subnet %s.\n", cidr)
	} else if ip := net.ParseIP(selector); ip != nil {
		host := db.FindHost(ip)
		if host == nil {
			fatal("Host with IP address %s not found in database", ip)
		}
		for k, v := range attrs {
			host.Attrs[k] = v
		}
		saveDB(dbPath, db)
		fmt.Printf("Edited host %s.\n", host.Name)
	} else {
		fatal("Invalid selector %s, must be an IP address or a CIDR prefix", selector)
	}
}

func RmAttr(dbPath string, db *database.DB, argv []string) {
	args := parse(subUsage["rmattr"], argv, false)
	keys := args["<key>"].([]string)
	selector := args["<cidr>"].(string)
	_, cidr, err := net.ParseCIDR(selector)
	if err == nil {
		alloc := db.FindAllocation(&database.IPNet{cidr}, true)
		if alloc == nil {
			fatal("Subnet %s not found in database", cidr)
		}
		for _, k := range keys {
			delete(alloc.Attrs, k)
		}
		saveDB(dbPath, db)
		fmt.Printf("Edited subnet %s.\n", cidr)
	} else if ip := net.ParseIP(selector); ip != nil {
		host := db.FindHost(ip)
		if host == nil {
			fatal("Host with IP address %s not found in database", ip)
		}
		for _, k := range keys {
			delete(host.Attrs, k)
		}
		saveDB(dbPath, db)
		fmt.Printf("Edited host %s.\n", host.Name)
	} else {
		fatal("Invalid selector %s, must be an IP address or a CIDR prefix", selector)
	}
}

func Server(dbPath string, db *database.DB, argv []string) {
	args := parse(subUsage["server"], argv, false)
	var host string
	if h, ok := args["--addr"].(string); ok {
		host = h
	}
	port := args["<port>"].(string)
	runServer(fmt.Sprintf("%s:%s", host, port), dbPath, db)
}

func fatal(s string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, s+"\n", args...)
	os.Exit(1)
}

func parse(usage string, argv []string, optFirst bool) map[string]interface{} {
	args, err := docopt.Parse(usage, argv, true, "gipam version 0.1", optFirst)
	if err != nil {
		fatal("Commandline parser failure: %s", err)
	}
	return args
}

func getDB(dbPath string) *database.DB {
	db, err := database.Load(dbPath)
	if err != nil {
		fatal("Couldn't load database: %s", err)
	}
	return db
}

func saveDB(dbPath string, db *database.DB) {
	if err := db.Save(dbPath); err != nil {
		fatal("Error while saving DB, change not committed: %s", err)
	}
}

func allocsAsTree(allocs []*database.Allocation, indent string) {
	for _, a := range allocs {
		fmt.Printf("%s%s (%s)\n", indent, a.Net, a.Name)
		allocsAsTree(a.Children, indent+"  ")
	}
}

func addr(s string) net.IP {
	ip := net.ParseIP(s)
	if ip == nil {
		fatal("Invalid IP address %s", s)
	}
	return ip
}

func cidr(s string) *database.IPNet {
	_, net, err := net.ParseCIDR(s)
	if err != nil {
		fatal("Invalid CIDR prefix %s", s)
	}
	return &database.IPNet{net}
}

func duration(s string) time.Duration {
	ret, err := time.ParseDuration(s)
	if err != nil {
		fatal("Invalid duration %s", err)
	}
	return ret
}

func attrs(ks []string, vs []string) map[string]string {
	ret := make(map[string]string, len(ks))
	for i := range ks {
		ret[ks[i]] = vs[i]
	}
	return ret
}

func sortedAttrKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
