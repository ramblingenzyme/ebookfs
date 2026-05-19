package main

import (
	"flag"
	"log"
	"net"
)

func main() {
	addr := flag.String("addr", "localhost:9999", "listen address")
	flag.Parse()
	_, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving 9P on %s", *addr)
}
