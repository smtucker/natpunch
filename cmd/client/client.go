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

	api "natpunch/proto"

	"google.golang.org/protobuf/proto"
)

// ReadTimeout defines the timeout for reading from the UDP connection.
const ReadTimeout = 1 * time.Second

// KeepAliveInterval defines the interval for sending keep-alive messages.
const KeepAliveInterval = 2 * time.Second

// Client represents the NAT traversal client.
type Client struct {
	AvailablePeers []*Peer
	KnownPeers     map[string]*Peer
	conn           *net.UDPConn
	srvAddr        *net.UDPAddr
	pubAddr        *net.UDPAddr
	localAddr      *net.UDPAddr
	stop           chan struct{}
	wg             sync.WaitGroup
	id             string
}

// getLocalAddr retrieves the local UDP address.
func getLocalAddr() *net.UDPAddr {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		panic(err)
	}

	// Log all non-loopback IPv4 addresses
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

// listen listens for incoming UDP messages.
func (c *Client) listen() {
	defer c.wg.Done()
	buf := make([]byte, 1024)

	for {
		c.conn.SetReadDeadline(time.Now().Add(ReadTimeout))
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

			switch content := msg.Content.(type) {
			case *api.Message_ClientListResponse:
				c.receiveClientList(content.ClientListResponse)
			case *api.Message_ConnectRequest:
				c.handleConnectRequest(content.ConnectRequest, addr)
			case *api.Message_ConnectResponse:
				c.handleConnectResponse(content.ConnectResponse, addr)
			case *api.Message_ConnectionEstablished:
				c.handleConnectionEstablished(content.ConnectionEstablished, addr)
			default:
				fmt.Println("Received:", string(buf[:n]))
			}
		}
	}
}

// readStdin reads commands from standard input.
func (c *Client) readStdin() {
	defer c.wg.Done()
	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		text := scanner.Text()
		tokens := strings.Fields(text)

		switch tokens[0] {
		case "exit":
			close(c.stop)
			return
		case "list":
			c.sendClientListRequest()
		case "connect":
			index, err := strconv.Atoi(tokens[1])
			if err != nil {
				log.Println("Invalid index:", err)
				continue
			}

			if index < 0 || index >= len(c.AvailablePeers) {
				log.Println("Index out of range")
				continue
			}
			c.connectToPeer(c.AvailablePeers[index])
		default:
			log.Println("Unknown command:", tokens[0])
		}
	}
}

// keepAliveServer sends keep-alive messages to the server.
func (c *Client) keepAliveServer() {
	ticker := time.NewTicker(KeepAliveInterval)
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
			return
		}
	}
}

// sendClientListRequest sends a request to the server for the client list.
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

// receiveClientList processes the client list received from the server.
func (c *Client) receiveClientList(resp *api.ClientListResponse) {
	fmt.Println("Client List:")
	c.AvailablePeers = make([]*Peer, len(resp.Clients))
	for i, client := range resp.Clients {
		c.AvailablePeers[i] = &Peer{
			addr: &net.UDPAddr{
				IP:   net.ParseIP(client.PublicEndpoint.IpAddress),
				Port: int(client.PublicEndpoint.Port),
			},
			id: client.ClientId,
		}
		fmt.Printf("%d - %s: %s:%d\n", i, client.ClientId, client.PublicEndpoint.IpAddress, client.PublicEndpoint.Port)
	}
}

// Run starts the client.
func (c *Client) Run(addr string, port string) {
	var err error
	c.srvAddr, err = net.ResolveUDPAddr("udp", addr+":"+port)
	if err != nil {
		log.Fatalf("cannot resolve server address: %s", err)
	}

	c.stop = make(chan struct{})

	c.localAddr = getLocalAddr()

	c.conn, err = net.ListenUDP("udp", c.localAddr)
	if err != nil {
		panic(err)
	}

	// Update local address with the port chosen by the OS
	localAddrWithPort, ok := c.conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		log.Fatalf("could not get local UDP address")
	}
	c.localAddr = localAddrWithPort

	if err := c.register(c.localAddr); err != nil {
		log.Println("Error registering:", err)
		return
	}

	c.KnownPeers = make(map[string]*Peer)

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
