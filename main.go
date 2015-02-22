package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"sort"

	kp "gopkg.in/alecthomas/kingpin.v1"

	"github.com/danderson/gipam/database"
)

func main() {
	// Global flags.
	dbPath := kp.Flag("db", "Path to the database file").Default("db").ExistingFile()
	debug := kp.Flag("debug", "Enable debugging output in CLI mode").Hidden().Default("false").Bool()

	// Server mode
	server := kp.Command("server", "Run the web server.")
	serverAddr := server.Arg("ip:port", "IP and port to listen on").Required().TCP()

	var allocArgs struct {
		cidr        *database.IPNet
		name        string
		attrs       map[string]string
		attrKeys    []string
		delChildren bool
	}
	{
		a := kp.Command("alloc", "")

		a.Command("list", "List allocated IP ranges.")

		// Helper for functions that take a CIDR prefix as their first
		// positional argument.
		cmd := func(parent *kp.CmdClause, name, help string) *kp.CmdClause {
			c := parent.Command(name, help)
			c.Arg("cidr", "CIDR prefix").Required().Dispatch(kp.Dispatch(func(p *kp.ParseContext) error {
				_, net, err := net.ParseCIDR(p.Peek().String())
				if err != nil {
					return err
				}
				allocArgs.cidr = &database.IPNet{net}
				return nil

			})).String()
			return c
		}

		c := cmd(a, "show", "Show details about an IP range.")

		c = cmd(a, "add", "Allocate a new IP range.")
		c.Arg("name", "Subnet name").Required().StringVar(&allocArgs.name)
		c.Arg("attrs", "key=value attributes").StringMapVar(&allocArgs.attrs)

		c = cmd(a, "rm", "Remove an IP range.")
		c.Flag("del-children", "Delete child subnets as well.").Default("false").BoolVar(&allocArgs.delChildren)

		c = cmd(a, "name", "Set the name of an IP range.")
		c.Arg("name", "Subnet name").Required().StringVar(&allocArgs.name)

		aa = a.Command("attr", "")
		c = cmd(aa, "set", "Set attributes on an IP range.")
		c.Arg("attrs", "key=value attributes").Required().StringMapVar(&allocArgs.attrs)

		c = cmd(aa, "rm", "Delete attributes from an IP range.")
		c.Arg("attr-keys", "Attribute keys").Required().StringsVar(&allocArgs.attrKeys)
	}

	var hostArgs struct {
		addr  net.IP
		name  string
		attrs map[string]string
	}
	{
		a := kp.Command("host", "")

		a.Command("list", "List hosts.")

		cmd := func(parent *kp.CmdClause, name, help string) *kp.CmdClause {
			c := parent.Command(name, help)
			c.Arg("addr", "An IP address of the host").Required().IPVar(&hostArgs.addr)
			return c
		}

		cmd(a, "show", "Show details about a host.")

		c := cmd(a, "add", "Add a host.")
		c.Arg("addrs", "Initial IP address").Required().IPVar(&hostArgs.addr)
	}

	// Host management
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
		runServer(*serverAddr, *dbPath, db)

	case "alloc add":
		err := db.AddAllocation(name, cidr, attrs)
		kp.FatalIfError(err, "Failed to add allocation")
		err = db.Save(*dbPath)
		kp.FatalIfError(err, "Error while saving DB, change not committed")
		fmt.Printf("Allocated \"%s\" at %s.\n", name, cidr)

	case "alloc desc":
		alloc := db.FindAllocation(cidr, true)
		if alloc == nil {
			kp.Fatalf("Alloc %s not found in database.", cidr)
		}
		alloc.Name = *allocDescDesc
		err = db.Save(*dbPath)
		kp.FatalIfError(err, "Error while saving DB, change not committed")
		fmt.Printf("Successfully edited %s.\n", cidr)

	case "alloc attr set":
		alloc := db.FindAllocation(cidr, true)
		if alloc == nil {
			kp.Fatalf("Alloc %s not found in database.", cidr)
		}
		for k, v := range attrs {
			alloc.Attrs[k] = v
		}
		err = db.Save(*dbPath)
		kp.FatalIfError(err, "Error while saving DB, change not committed")
		fmt.Printf("Successfully edited %s.\n", cidr)

	case "alloc attr rm":
		alloc := db.FindAllocation(cidr, true)
		if alloc == nil {
			kp.Fatalf("Alloc %s not found in database.", cidr)
		}
		for _, k := range *allocAttrDelAttrs {
			delete(alloc.Attrs, k)
		}
		err = db.Save(*dbPath)
		kp.FatalIfError(err, "Error while saving DB, change not committed")
		fmt.Printf("Successfully edited %s.\n", cidr)

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
	default:
		kp.Usage()
	}

}

type allocArgs struct {
}

func (a *allocArgs) CIDRArg() kp.Dispatch {
	return func(p *kp.ParseContext) error {
		_, net, err := net.ParseCIDR(p.Peek().String())
		if err != nil {
			return err
		}
		cidr = &database.IPNet{net}
		return nil
	}
}

func allocsAsTree(allocs []*database.Allocation, indent string) {
	for _, a := range allocs {
		fmt.Printf("%s%s (%s)\n", indent, a.Net, a.Name)
		allocsAsTree(a.Children, indent+"  ")
	}
}
