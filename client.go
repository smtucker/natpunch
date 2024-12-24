package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
)

var (
	mut      sync.Mutex
	peerList []ClientInfo
)

func client(port string, address string) {
	conn, err := net.ListenPacket("udp", ":0")
	if err != nil {
		log.Fatal("Error listening:", err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().String()
	fmt.Println("Local address:", localAddr)

	serverAddrStr := address + ":" + port
	serverAddr, err := net.ResolveUDPAddr("udp", serverAddrStr)
	if err != nil {
		log.Fatal("Error resolving peer address:", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Start listening goroutine
	go func() {
		defer wg.Done()
		buffer := make([]byte, 1024)
		for {
			n, addr, err := conn.ReadFrom(buffer)
			if err != nil {
				log.Println("Error reading:", err)
				continue // Don't exit on read error
			}

			message := Message{}
			err = json.Unmarshal(buffer[:n], &message)
			if err != nil {
				log.Println("Error unmarshaling message:", err)
				log.Println("Received:", string(buffer[:n]))
				continue
			}

			switch message.Command {
			case ClientListResponseMessage.Command:
				mut.Lock()
				err = json.Unmarshal([]byte(message.Payload), &peerList)
				mut.Unlock()
				if err != nil {
					log.Println("Error unmarshaling client list:", err)
					continue
				}
				fmt.Println("Client list:", peerList)
			case "chat":
				fmt.Printf("Chat from %s: %s\n", addr, message.Payload)
			default:
				fmt.Printf("Received from %s: %s\n", addr, string(buffer[:n]))
			}
		}
	}()

	// Start sending goroutine
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(os.Stdin)
		for {
			message, _ := reader.ReadString('\n')
			message = strings.TrimSpace(message)

			// Check for commands
			switch message {
			case "list":
				_, err = conn.WriteTo(ClientListRequestBytes, serverAddr)
				if err != nil {
					fmt.Println("Error sending client list request:", err)
				}
				continue
			default:
				messageClientList(conn, peerList, message)
				messageClientList(conn, []ClientInfo{{Addr: serverAddr}}, message)
			}
		}
	}()

	wg.Wait()
}

func messageClientList(conn net.PacketConn, clientList []ClientInfo, message string) {

	msg := Message{Command: "chat", Payload: message}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Println("Error marshaling message:", err)
		return
	}

	for _, client := range clientList {
		_, err = conn.WriteTo(msgBytes, client.Addr)
		if err != nil {
			fmt.Printf("Error sending chat to %s: %s", client.Addr, err)
		}
	}
}
