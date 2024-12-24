package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
)

var (
	clients = make(map[string]*ClientInfo)
	mu      sync.Mutex
)

// Server ...
func Server(port string) {
	listenAddr, err := net.ResolveUDPAddr("udp", ":"+port)
	if err != nil {
		log.Fatal(err)
	}

	conn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	fmt.Println("Server listening on :8080")

	buffer := make([]byte, 1024)

	for {
		length, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Println("Error reading:", err)
			continue
		}

		clientAddrStr := addr.String()

		// TODO: Add this to a register command
		if _, ok := clients[clientAddrStr]; !ok {
			mu.Lock()
			clients[clientAddrStr] = &ClientInfo{Addr: addr}
			fmt.Printf("Client connected: %s\n", clientAddrStr)
			mu.Unlock()
		}

		cmdMessage := Message{}
		err = json.Unmarshal(buffer[:length], &cmdMessage)
		if err != nil {
			fmt.Println("Error unmarshaling message:", err)
			fmt.Println("Full message: ", string(buffer))
			continue
		}

		switch cmdMessage.Command {
		case "chat":
			fmt.Printf("Received chat message from %s: %s\n", addr, cmdMessage.Payload)
		case "clientListRequest":
			fmt.Println("Received client list request from:", addr)
			sendClientList(conn, addr)
    default:
      fmt.Println("Unknown command:", string(buffer))
		}
	}
}

func sendClientList(conn *net.UDPConn, addr *net.UDPAddr) {
	mu.Lock()
	defer mu.Unlock()

	clientList := make([]ClientInfo, 0, len(clients))
	for _, client := range clients {
		clientList = append(clientList, *client)
	}
	clientListBytes, err := json.Marshal(clientList)
	if err != nil {
		log.Println("Error marshaling client list:", err)
	}

	clientListMessage := Message{Command: ClientListResponseMessage.Command, Payload: string(clientListBytes)}
	messageBytes, err := json.Marshal(clientListMessage)
	if err != nil {
		log.Println("Error marshaling client list message:", err)
	}

	_, err = conn.WriteToUDP(messageBytes, addr)
	if err != nil {
		log.Printf("Error sending client list to %s: %v\n", addr, err)
	}
}
