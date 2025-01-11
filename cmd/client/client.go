package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	api "natpunch/proto/gen/go"

	"google.golang.org/protobuf/proto"
)

// Client is the client
type Client struct {
	AvailablePeers []*peer
	KnownPeers     map[string]*peer
	conn           *net.UDPConn
	srvAddr        *net.UDPAddr
	pubAddr        *net.UDPAddr
	stop           chan struct{}
	wg             sync.WaitGroup
	id             string
}

func getLocalAddr() *net.UDPAddr {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		panic(err)
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			log.Println("Local address found:", ipNet.IP)
		}
	}
	if len(addrs) == 0 {
		log.Fatal("No local address found")
	}
	if len(addrs) > 1 {
		fmt.Println("Found multiple local addresses, select one to use:")
		for i, addr := range addrs {
			fmt.Printf("%d - %s\n", i, addr)
		}
		fmt.Print("Select address: ")
		var selected int
		fmt.Scan(&selected)
		if selected < 0 || selected >= len(addrs) {
			log.Fatal("Invalid address selected")
		}
		return &net.UDPAddr{IP: addrs[selected].(*net.IPNet).IP, Port: 0}
	}
	return &net.UDPAddr{IP: addrs[0].(*net.IPNet).IP, Port: 0}
}

func (c *Client) listen() {
	defer c.wg.Done()
	buf := make([]byte, 1024)
	for {
		c.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		select {
		case <-c.stop:
			return
		default:
			n, addr, err := c.conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				log.Println("Error reading:", err)
				return
			}

			msg := &api.Message{}
			if err := proto.Unmarshal(buf[:n], msg); err != nil {
				log.Println("Failed to unmarshal message:", err)
				return
			}

			if listResp, ok := msg.Content.(*api.Message_ClientListResponse); ok {
				// TODO: Make sure this came from the server
				c.recieveClientList(listResp.ClientListResponse)
				continue
			}

			if connectReq, ok := msg.Content.(*api.Message_ConnectRequest); ok {
				c.handleConnectRequest(connectReq.ConnectRequest, addr)
				continue
			}

			if connectResp, ok := msg.Content.(*api.Message_ConnectResponse); ok {
				c.handleConnectResponse(connectResp.ConnectResponse, addr)
				continue
			}

			if connectionEstablished, ok := msg.Content.(*api.Message_ConnectionEstablished); ok {
				c.handleConnectionEstablished(connectionEstablished.ConnectionEstablished, addr)
				continue
			}

			fmt.Println("Received:", string(buf[:n]))
		}
	}
}

func (c *Client) readStdin() {
	defer c.wg.Done()
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		text := scanner.Text()
		tokens := strings.Fields(text)
		switch tokens[0] {
		case "exit":
			close(c.stop) // Signal other goroutines to stop
			return
		case "list":
			c.sendClientListRequest()
		case "connect":
			index, err := strconv.Atoi(tokens[1])
			if err != nil {
				log.Println("Invalid index:", err)
				continue
			}
			c.peerConnect(c.AvailablePeers[index])
		default:
			log.Println("Unknown command:", tokens[0])
		}

	}
}

func (c *Client) keepAliveServer() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	msg := &api.Message{
		Content: &api.Message_KeepAlive{
			KeepAlive: &api.KeepAlive{
				ClientId: c.id,
			},
		},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Println("Failed to marshal keepalive:", err)
	}

	for {
		select {
		case <-ticker.C:
			_, err = c.conn.WriteTo(out, c.srvAddr)
			if err != nil {
				log.Println("Failed to send keepalive:", err)
			}
		case <-c.stop:
			c.wg.Done()
			return
		}
	}
}

func (c *Client) sendClientListRequest() {
	req := &api.ClientListRequest{}
	msg := &api.Message{
		Content: &api.Message_ClientListRequest{ClientListRequest: req},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Println("Failed to marshal list request:", err)
		return
	}

	_, err = c.conn.WriteTo(out, c.srvAddr)
	if err != nil {
		log.Println("Failed to send list request:", err)
	}
}

func (c *Client) recieveClientList(resp *api.ClientListResponse) {
	fmt.Println("Client List:")
	c.AvailablePeers = make([]*peer, len(resp.Clients))
	for i, client := range resp.Clients {
		c.AvailablePeers[i] = &peer{
			addr: &net.UDPAddr{
				IP:   net.ParseIP(client.PublicEndpoint.IpAddress),
				Port: int(client.PublicEndpoint.Port),
			},
			id: client.ClientId,
		}
		fmt.Printf("%d - %s: %s:%d\n", i, client.ClientId, client.PublicEndpoint.IpAddress, client.PublicEndpoint.Port)
	}
}

// Run is the main method that initializes and starts running the client
func (c *Client) Run(addr string, port string) {
	var err error
	c.srvAddr, err = net.ResolveUDPAddr("udp", addr+":"+port)
	if err != nil {
		log.Fatalf("cannot resolve server address: %s", err)
	}

	c.stop = make(chan struct{})

	c.pubAddr = getLocalAddr()

	c.conn, err = net.ListenUDP("udp", c.pubAddr)
	if err != nil {
		panic(err)
	}

	if err := c.register(c.pubAddr); err != nil {
		log.Println("Error registering:", err)
		return
	}

	c.KnownPeers = make(map[string]*peer)

	c.wg = sync.WaitGroup{}

	c.wg.Add(1)
	go c.listen()

	c.wg.Add(1)
	go c.readStdin()

	c.wg.Add(1)
	go c.keepAliveServer()

	<-c.stop
	log.Println("Closing connection")

	c.wg.Wait()
	c.conn.Close()
	log.Println("Client exited")
}

func main() {
	address := flag.String("a", "127.0.0.1", "Server address")
	port := flag.String("p", "8080", "Port to use")

	flag.Parse()

	client := &Client{}
	client.Run(*address, *port)
}
