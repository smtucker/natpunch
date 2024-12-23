package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
)

// ClientInfo ...
type ClientInfo struct {
	Addr *net.UDPAddr
}

// Message ...
type Message struct {
	Command string `json:"type"`
	Payload string `json:"payload"`
}

var ClientListRequestMessage = Message{Command: "clientListRequest"}
var ClientListResponseMessage = Message{Command: "clientListResponse"}

// ClientListRequestBytes ...
// Premade JSON message to request client list
var ClientListRequestBytes, _ = json.Marshal(ClientListRequestMessage)

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
		Server(*port)
	case "client":
		client(*port, *address)
	default:
		fmt.Println("Invalid command:", command)
		os.Exit(1)
	}
}
