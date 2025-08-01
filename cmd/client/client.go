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

// ReadTimeout defines the timeout for reading from the UDP connection.
const ReadTimeout = 1 * time.Second

// KeepAliveInterval defines the interval for sending keep-alive messages.
const KeepAliveInterval = 2 * time.Second

// Client represents the NAT traversal client.
type Client struct {
	AvailablePeers   []*Peer
	KnownPeers       map[string]*Peer
	AvailableLobbies []*LobbyInfo
	CurrentLobby     *LobbyInfo
	conn             *net.UDPConn
	srvAddr          *net.UDPAddr
	pubAddr          *net.UDPAddr
	localAddr        *net.UDPAddr
	stop             chan struct{}
	wg               sync.WaitGroup
	id               string
}

// LobbyInfo represents lobby information for the client
type LobbyInfo struct {
	ID             string
	Name           string
	HostClientID   string
	CurrentPlayers uint32
	MaxPlayers     uint32
	Members        []*api.ClientInfo
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
			case *api.Message_Error:
				log.Printf("Server error: %s", content.Error.Message)
			case *api.Message_DebugMessage:
				// Respond to ping messages
				if content.DebugMessage.Message == "PING" {
					pongMsg := &api.Message{
						Content: &api.Message_DebugMessage{
							DebugMessage: &api.DebugMessage{Message: "PONG"},
						},
					}
					pongBytes, _ := proto.Marshal(pongMsg)
					c.conn.WriteTo(pongBytes, addr)
				}
			case *api.Message_CreateLobbyResponse:
				c.handleCreateLobbyResponse(content.CreateLobbyResponse)
			case *api.Message_JoinLobbyResponse:
				c.handleJoinLobbyResponse(content.JoinLobbyResponse)
			case *api.Message_LobbyListResponse:
				c.handleLobbyListResponse(content.LobbyListResponse)
			case *api.Message_LobbyUpdate:
				c.handleLobbyUpdate(content.LobbyUpdate)
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
		case "lobbies":
			c.sendLobbyListRequest()
		case "create":
			if len(tokens) < 3 {
				log.Println("Usage: create <lobby_name> <max_players>")
				continue
			}
			maxPlayers, err := strconv.ParseUint(tokens[2], 10, 32)
			if err != nil {
				log.Println("Invalid max players:", err)
				continue
			}
			c.createLobby(tokens[1], uint32(maxPlayers))
		case "join":
			if len(tokens) < 2 {
				log.Println("Usage: join <lobby_index>")
				continue
			}
			index, err := strconv.Atoi(tokens[1])
			if err != nil {
				log.Println("Invalid index:", err)
				continue
			}
			if index < 0 || index >= len(c.AvailableLobbies) {
				log.Println("Lobby index out of range")
				continue
			}
			c.joinLobby(c.AvailableLobbies[index])
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

func (c *Client) sendLobbyListRequest() {
	req := &api.LobbyListRequest{
		RequestId: fmt.Sprintf("req_%d", time.Now().UnixNano()),
	}
	msg := &api.Message{
		Content: &api.Message_LobbyListRequest{LobbyListRequest: req},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Println("Failed to marshal lobby list request:", err)
		return
	}

	_, err = c.conn.WriteTo(out, c.srvAddr)
	if err != nil {
		log.Println("Failed to send lobby list request:", err)
	}
}

func (c *Client) createLobby(name string, maxPlayers uint32) {
	req := &api.CreateLobbyRequest{
		LobbyName:  name,
		ClientId:   c.id,
		MaxPlayers: maxPlayers,
	}
	msg := &api.Message{
		Content: &api.Message_CreateLobbyRequest{CreateLobbyRequest: req},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Println("Failed to marshal create lobby request:", err)
		return
	}

	_, err = c.conn.WriteTo(out, c.srvAddr)
	if err != nil {
		log.Println("Failed to send create lobby request:", err)
	}
}

func (c *Client) joinLobby(lobby *LobbyInfo) {
	req := &api.JoinLobbyRequest{
		LobbyId:  lobby.ID,
		ClientId: c.id,
	}
	msg := &api.Message{
		Content: &api.Message_JoinLobbyRequest{JoinLobbyRequest: req},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Println("Failed to marshal join lobby request:", err)
		return
	}

	_, err = c.conn.WriteTo(out, c.srvAddr)
	if err != nil {
		log.Println("Failed to send join lobby request:", err)
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

func (c *Client) handleCreateLobbyResponse(resp *api.CreateLobbyResponse) {
	if resp.Success {
		fmt.Printf("✅ Lobby created successfully! Lobby ID: %s\n", resp.LobbyId)
	} else {
		fmt.Printf("❌ Failed to create lobby: %s\n", resp.Message)
	}
}

func (c *Client) handleJoinLobbyResponse(resp *api.JoinLobbyResponse) {
	if resp.Success {
		fmt.Printf("✅ Successfully joined lobby %s!\n", resp.LobbyId)
		fmt.Printf("📋 Lobby members (%d):\n", len(resp.LobbyMembers))
		for i, member := range resp.LobbyMembers {
			fmt.Printf("  %d - %s: %s:%d\n", i, member.ClientId, member.PublicEndpoint.IpAddress, member.PublicEndpoint.Port)
		}

		// Convert to local LobbyInfo
		c.CurrentLobby = &LobbyInfo{
			ID:             resp.LobbyId,
			Members:        resp.LobbyMembers,
			CurrentPlayers: uint32(len(resp.LobbyMembers)),
		}
	} else {
		fmt.Printf("❌ Failed to join lobby: %s\n", resp.Message)
	}
}

func (c *Client) handleLobbyListResponse(resp *api.LobbyListResponse) {
	if resp.Success {
		fmt.Println("📋 Available Lobbies:")
		c.AvailableLobbies = make([]*LobbyInfo, len(resp.Lobbies))
		for i, lobby := range resp.Lobbies {
			c.AvailableLobbies[i] = &LobbyInfo{
				ID:             lobby.LobbyId,
				Name:           lobby.LobbyName,
				HostClientID:   lobby.HostClientId,
				CurrentPlayers: lobby.CurrentPlayers,
				MaxPlayers:     lobby.MaxPlayers,
				Members:        lobby.Members,
			}
			fmt.Printf("%d - %s (Host: %s, Players: %d/%d)\n",
				i, lobby.LobbyName, lobby.HostClientId, lobby.CurrentPlayers, lobby.MaxPlayers)
		}
	} else {
		fmt.Printf("❌ Failed to get lobby list: %s\n", resp.Message)
	}
}

func (c *Client) handleLobbyUpdate(update *api.LobbyUpdate) {
	fmt.Printf("🔄 Lobby update for %s: %s\n", update.LobbyId, update.UpdateType)

	// Update current lobby if it matches
	if c.CurrentLobby != nil && c.CurrentLobby.ID == update.LobbyId {
		c.CurrentLobby = &LobbyInfo{
			ID:             update.LobbyInfo.LobbyId,
			Name:           update.LobbyInfo.LobbyName,
			HostClientID:   update.LobbyInfo.HostClientId,
			CurrentPlayers: update.LobbyInfo.CurrentPlayers,
			MaxPlayers:     update.LobbyInfo.MaxPlayers,
			Members:        update.LobbyInfo.Members,
		}

		fmt.Printf("📋 Updated lobby members (%d):\n", len(update.LobbyInfo.Members))
		for i, member := range update.LobbyInfo.Members {
			fmt.Printf("  %d - %s: %s:%d\n", i, member.ClientId, member.PublicEndpoint.IpAddress, member.PublicEndpoint.Port)
		}
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
