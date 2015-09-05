package main

import (
	"flag"
	"fmt"
	"log"
)

var (
	port   = flag.Int("port", 8000, "Port on which to serve GIPAM")
	addr   = flag.String("addr", "", "Address to listen on")
	dbPath = flag.String("db", "gipam.db", "Database file to use")
	debug  = flag.Bool("debug", false, "Format JSON responses nicely")
)

func main() {
	flag.Parse()
	log.Fatalln(runServer(fmt.Sprintf("%s:%d", *addr, *port), *dbPath))
}
