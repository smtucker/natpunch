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

func client(port string, address string) {
	conn, err := net.ListenPacket("udp", ":0")
	if err != nil {
		log.Fatal("Error listening:", err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().String()
	fmt.Println("Local address:", localAddr)

	peerAddrStr := address + ":" + port
	fmt.Println("Resolving address:", peerAddrStr)
	peerAddr, err := net.ResolveUDPAddr("udp", peerAddrStr)
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
				var clientList []ClientInfo
				err = json.Unmarshal([]byte(message.Payload), &clientList)
				if err != nil {
					log.Println("Error unmarshaling client list:", err)
					continue
				}
				fmt.Println("Client list:", clientList)
				pingClientList(conn, clientList)
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
				_, err = conn.WriteTo(ClientListRequestBytes, peerAddr)
				if err != nil {
					fmt.Println("Error sending client list request:", err)
				}
				continue
			default:
				var chatMsg = Message{Command: "chat", Payload: message}
				chatMsgBytes, err := json.Marshal(chatMsg)
				if err != nil {
					fmt.Println("Error marshaling chat message:", err)
					continue
				}
				_, err = conn.WriteTo(chatMsgBytes, peerAddr)
				if err != nil {
					log.Println("Error sending:", err)
					break // Exit sending loop on error
				}
			}
		}
	}()

	wg.Wait()
}

func pingClientList(conn net.PacketConn, clientList []ClientInfo) {
	fmt.Println("Pinging all clients...")
	for _, client := range clientList {
    msg := Message{Command: "chat", Payload: "Hello there!"}
    msgBytes, err := json.Marshal(msg)
    if err != nil {
      log.Println("Error marshaling message:", err)
      continue
    }
		conn.WriteTo(msgBytes, client.Addr)
	}
}
