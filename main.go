package main

import (
	"flag"
	"fmt"
	"os"

	"natpunch/interal/client"
	"natpunch/interal/server"
)

func main() {
	address := flag.String("a", "udp", "Server address (for client mode)")
	port := flag.String("p", "8080", "Port to use")

	flag.Parse()

	if len(flag.Args()) != 1 {
		fmt.Println("Usage: go run main.go [serve|connect]")
		os.Exit(1)
	}

	command := flag.Arg(0)

	switch command {
	case "serve":
		srv := &server.Server{}
		srv.Run(*address, *port)
	case "client":
		client := &client.Client{}
		client.Run(*address, *port)
	default:
		fmt.Println("Invalid command:", command)
		os.Exit(1)
	}
}
