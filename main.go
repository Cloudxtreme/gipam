package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"sort"
	"strings"

	kp "gopkg.in/alecthomas/kingpin.v1"

	"github.com/danderson/gipam/database"
)

func main() {
	// Some common arguments used by many of these commands
	var (
		cidr  *database.IPNet
		addr  net.IP
		addrs []net.IP
		name  string
		attrs = make(map[string]string)
	)

	var (
		cidrArg = func(c *kp.CmdClause) {
			c.Arg("cidr", "CIDR prefix").Required().Dispatch(kp.Dispatch(func(p *kp.ParseContext) error {
				_, net, err := net.ParseCIDR(p.Peek().String())
				if err != nil {
					return err
				}
				cidr = &database.IPNet{net}
				return nil
			})).String()
		}
		addrsArg = func(c *kp.CmdClause) {
			c.Arg("addrs", "Comma-separated IP addresses").Required().Dispatch(kp.Dispatch(func(p *kp.ParseContext) error {
				ipStrs := strings.Split(p.Peek().String(), ",")
				for _, ipStr := range ipStrs {
					ip := net.ParseIP(ipStr)
					if ip == nil {
						return fmt.Errorf("Invalid IP address '%s'", ipStr)
					}
					addrs = append(addrs, ip)
				}
				if len(addrs) < 1 {
					return fmt.Errorf("Must specify at least one IP address")
				}
				return nil
			})).String()
		}
		addrArg = func(c *kp.CmdClause) {
			c.Arg("addr", "IP address").Required().IPVar(&addr)
		}
		nameArg = func(c *kp.CmdClause) {
			c.Arg("name", "Object name").Required().StringVar(&name)
		}
		attrArg = func(c *kp.CmdClause) {
			c.Arg("attrs", "Key-value attributes").StringMapVar(&attrs)
		}
	)

	dbPath := kp.Flag("db", "Path to the database file").Default("db").ExistingFile()
	debug := kp.Flag("debug", "Enable debugging output in CLI mode").Hidden().Default("false").Bool()

	// Server mode
	server := kp.Command("server", "Run the web server.")
	serverAddr := server.Arg("ip:port", "IP and port to listen on").Required().TCP()

	// Alloc command family
	alloc := kp.Command("alloc", "IP range allocation management.")

	allocAdd := alloc.Command("add", "Allocate a new IP range.")
	cidrArg(allocAdd)
	nameArg(allocAdd)
	attrArg(allocAdd)

	allocEdit := alloc.Command("edit", "Edit the name/attributes of an IP range.")
	cidrArg(allocEdit)
	nameArg(allocEdit)
	attrArg(allocEdit)

	allocDel := alloc.Command("rm", "Remove an IP range.")
	cidrArg(allocDel)
	allocDelChildren := allocDel.Flag("delete-children", "Delete sub-allocations, instead of reparenting them").Default("false").Bool()

	alloc.Command("list", "List allocated IP ranges.")
	allocShow := alloc.Command("show", "Show detailed data about an IP range.")
	cidrArg(allocShow)

	// Host command family
	host := kp.Command("host", "Host allocation management.")
	hostAdd := host.Command("add", "Allocate a new host.")
	nameArg(hostAdd)
	addrsArg(hostAdd)
	attrArg(hostAdd)

	hostEdit := host.Command("edit", "Edit the name/addrs/attributes of a host.")
	addrArg(hostEdit)
	nameArg(hostEdit)
	addrsArg(hostEdit)
	attrArg(hostEdit)

	hostRm := host.Command("rm", "Remove a host.")
	addrArg(hostRm)

	host.Command("list", "List hosts.")
	hostShow := host.Command("show", "Show detailed data about a host.")
	addrArg(hostShow)

	cmd := kp.Parse()

	if cmd != "server" && !*debug {
		log.SetOutput(ioutil.Discard)
	}

	db, err := database.Load(*dbPath)
	kp.FatalIfError(err, "Couldn't load database")

	switch cmd {
	case "server":
		runServer(*serverAddr, db)

	case "alloc add":
		err := db.AddAllocation(name, cidr, attrs)
		kp.FatalIfError(err, "Failed to add allocation")
		err = db.Save(*dbPath)
		kp.FatalIfError(err, "Error while saving DB, change not committed")
		fmt.Printf("Allocated \"%s\" at %s.\n", name, cidr)
	case "alloc edit":
		alloc := db.FindAllocation(cidr, true)
		if alloc == nil {
			kp.Fatalf("Alloc %s not found in database.", cidr)
		}
		err = db.RemoveAllocation(alloc, true)
		kp.FatalIfError(err, "Error while editing alloc")
		err = db.AddAllocation(name, cidr, attrs)
		kp.FatalIfError(err, "Error while editing alloc")
		err = db.Save(*dbPath)
		kp.FatalIfError(err, "Error while saving DB, change not committed")
		fmt.Printf("Successfully edited %s.\n", cidr)
	case "alloc rm":
		alloc := db.FindAllocation(cidr, true)
		if alloc == nil {
			kp.Fatalf("Alloc %s not found in database.", cidr)
		}
		err = db.RemoveAllocation(alloc, !*allocDelChildren)
		kp.FatalIfError(err, "Failed to delete alloc")
		err = db.Save(*dbPath)
		kp.FatalIfError(err, "Error while saving DB, change not committed")
		fmt.Printf("Deleted %s", cidr)
		if *allocDelChildren {
			fmt.Printf(" and children")
		}
		fmt.Printf(".\n")
	case "alloc list":
		allocsAsTree(db.Allocs, "")
	case "alloc show":
		alloc := db.FindAllocation(cidr, true)
		if alloc == nil {
			kp.Fatalf("Alloc %s not found in database.", cidr)
		}
		fmt.Printf("Name: %s\nPrefix: %s\n", alloc.Name, alloc.Net)
		keys := make([]string, 0, len(alloc.Attrs))
		for k := range alloc.Attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("%s: %s\n", k, alloc.Attrs[k])
		}

	case "host add":
		err := db.AddHost(name, addrs, attrs)
		kp.FatalIfError(err, "Failed to add host")
		err = db.Save(*dbPath)
		kp.FatalIfError(err, "Error while saving DB, change not committed")
		fmt.Printf("Added host \"%s\"", name)
	case "host edit":
		host := db.FindHost(addr)
		if host == nil {
			kp.Fatalf("Host %s not found in database.", addr)
		}
		err = db.RemoveHost(host)
		kp.FatalIfError(err, "Error while editing host")
		err = db.AddHost(name, addrs, attrs)
		kp.FatalIfError(err, "Error while editing host")
		err = db.Save(*dbPath)
		kp.FatalIfError(err, "Error while saving DB, change not committed")
		fmt.Printf("Successfully edited host %s.\n", addr)
	case "host rm":
		host := db.FindHost(addr)
		if host == nil {
			kp.Fatalf("Host %s not found in database.", addr)
		}
		err = db.RemoveHost(host)
		kp.FatalIfError(err, "Failed to delete host")
		err = db.Save(*dbPath)
		kp.FatalIfError(err, "Error while saving DB, change not committed")
		fmt.Printf("Deleted host %s.\n", host.Name)
	case "host list":
		for _, h := range db.Hosts {
			fmt.Printf("%s\n", h.Name)
			for _, a := range h.Addrs {
				fmt.Printf("  %s\n", a)
			}
		}
	case "host show":
		host := db.FindHost(addr)
		if host == nil {
			kp.Fatalf("Host with address %s not found in database.", addr)
		}
		fmt.Printf("Name: %s\n", host.Name)
		for _, a := range host.Addrs {
			fmt.Printf("Addr: %s\n", a)
		}
		keys := make([]string, 0, len(host.Attrs))
		for k := range host.Attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("%s: %s\n", k, host.Attrs[k])
		}
	}
}

func allocsAsTree(allocs []*database.Allocation, indent string) {
	for _, a := range allocs {
		fmt.Printf("%s%s (%s)\n", indent, a.Net, a.Name)
		allocsAsTree(a.Children, indent+"  ")
	}
}
