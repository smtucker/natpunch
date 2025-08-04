package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"

	api "natpunch/proto/gen/go"
)

// ReadTimeout defines the timeout for reading from the UDP connection.
const ReadTimeout = 1 * time.Second

// KeepAliveInterval defines the interval for sending keep-alive messages.
const KeepAliveInterval = 2 * time.Second

// Client represents the NAT traversal client.
type Client struct {
	AvailableLobbies []*LobbyInfo
	CurrentLobby     *LobbyInfo
	pendingPings     map[uint32]time.Time
	conn             *net.UDPConn
	srvAddr          *net.UDPAddr
	pubAddr          *net.UDPAddr
	localAddr        *net.UDPAddr
	stop             chan struct{}
	stopOnce         sync.Once
	wg               sync.WaitGroup
	lobbyMutex       sync.RWMutex
	id               string
	tui              *tea.Program
	registered       chan bool
}

// LobbyInfo represents lobby information for the client
type LobbyInfo struct {
	ID             string
	Name           string
	HostClientID   string
	CurrentPlayers uint32
	MaxPlayers     uint32
	Members        []*api.ClientInfo
	Peers          map[string]*Peer
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

			case *api.Message_ConnectionInstruction:
				c.handleConnectionInstruction(content.ConnectionInstruction, addr)
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
					c.send(pongMsg)
				}
			case *api.Message_CreateLobbyResponse:
				c.handleCreateLobbyResponse(content.CreateLobbyResponse)
			case *api.Message_JoinLobbyResponse:
				c.handleJoinLobbyResponse(content.JoinLobbyResponse)
			case *api.Message_JoinLobbySuccess:
				c.handleJoinLobbySuccess(content.JoinLobbySuccess)
			case *api.Message_LobbyListResponse:
				c.handleLobbyListResponse(content.LobbyListResponse)
			case *api.Message_LobbyUpdate:
				c.handleLobbyUpdate(content.LobbyUpdate)
			case *api.Message_Ping:
				c.handlePing(content.Ping, addr)
			case *api.Message_Pong:
				c.handlePong(content.Pong, addr)
			case *api.Message_LeaveLobbyResponse:
				c.handleLeaveLobbyResponse(content.LeaveLobbyResponse)
			case *api.Message_PeerLeft:
				c.handlePeerLeft(content.PeerLeft)
			case *api.Message_RegisterResponse:
				c.handleRegisterResponse(content.RegisterResponse)
			default:
				log.Println("Received:", string(buf[:n]))
			}
		}
	}
}

// keepAliveServer sends keep-alive messages to the server.
func (c *Client) keepAliveServer() {
	defer c.wg.Done()
	ticker := time.NewTicker(KeepAliveInterval)
	defer ticker.Stop()

	msg := &api.Message{
		Content: &api.Message_KeepAlive{
			KeepAlive: &api.KeepAlive{
				ClientId: c.id,
			},
		},
	}

	for {
		select {
		case <-ticker.C:
			c.send(msg)
		case <-c.stop:
			return
		}
	}
}

func (c *Client) sendPing(peer *Peer) {
	seq := uint32(time.Now().UnixNano())
	ping := &api.Ping{
		SourceClientId:      c.id,
		DestinationClientId: peer.id,
		SequenceNumber:      seq,
	}
	msg := &api.Message{
		Content: &api.Message_Ping{Ping: ping},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Println("Failed to marshal ping:", err)
		return
	}

	_, err = c.conn.WriteTo(out, peer.addr)
	if err != nil {
		log.Println("Failed to send ping:", err)
	} else {
		log.Printf("Ping sent to %s", peer.id)
		c.pendingPings[seq] = time.Now()
	}
}

func (c *Client) handlePing(ping *api.Ping, addr *net.UDPAddr) {
	log.Printf("Received ping from %s", ping.SourceClientId)

	pong := &api.Pong{
		SourceClientId:      c.id,
		DestinationClientId: ping.SourceClientId,
		SequenceNumber:      ping.SequenceNumber,
	}
	msg := &api.Message{
		Content: &api.Message_Pong{Pong: pong},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Println("Failed to marshal pong:", err)
		return
	}

	_, err = c.conn.WriteTo(out, addr)
	if err != nil {
		log.Println("Failed to send pong:", err)
	}
}

func (c *Client) handlePong(pong *api.Pong, addr *net.UDPAddr) {
	if startTime, ok := c.pendingPings[pong.SequenceNumber]; ok {
		latency := time.Since(startTime)
		log.Printf("Received pong from %s, latency: %s", pong.SourceClientId, latency)
		delete(c.pendingPings, pong.SequenceNumber)
	} else {
		log.Printf("Received pong from %s with unknown sequence number", pong.SourceClientId)
	}
}

func (c *Client) isLobbyHost() bool {
	if c.CurrentLobby == nil {
		return false
	}
	return c.CurrentLobby.HostClientID == c.id
}

func (c *Client) send(msg *api.Message) {
	out, err := proto.Marshal(msg)
	if err != nil {
		log.Println("Failed to marshal message:", err)
		return
	}

	_, err = c.conn.WriteTo(out, c.srvAddr)
	if err != nil {
		log.Println("Failed to send message:", err)
	}
}

// Run starts the client.
func (c *Client) Run() {
	c.wg.Add(1)
	go c.listen()

	c.wg.Add(1)
	go c.runTUI()

	go func() {
		if err := c.register(c.localAddr); err != nil {
			log.Println("Error registering:", err)
			c.stopClient()
			return
		}

		c.wg.Add(1)
		go c.keepAliveServer()
	}()

	<-c.stop
	log.Println("Closing connection")

	c.wg.Wait()
	c.conn.Close()
	log.Println("Client exited")
}

func (c *Client) Init(addr string, port string) error {
	var err error
	c.srvAddr, err = net.ResolveUDPAddr("udp", addr+":"+port)
	if err != nil {
		return fmt.Errorf("cannot resolve server address: %w", err)
	}

	c.stop = make(chan struct{})
	c.registered = make(chan bool)

	c.localAddr = &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0}

	c.conn, err = net.ListenUDP("udp", c.localAddr)
	if err != nil {
		return fmt.Errorf("cannot listen on UDP port: %w", err)
	}

	// Update local address with the port chosen by the OS
	localAddrWithPort, ok := c.conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return fmt.Errorf("could not get local UDP address")
	}
	c.localAddr = localAddrWithPort

	c.pendingPings = make(map[uint32]time.Time)
	c.wg = sync.WaitGroup{}
	return nil
}

func (c *Client) handleRegisterResponse(resp *api.RegisterResponse) {
	if resp.Success {
		c.id = resp.ClientId
		log.Println("Registered with server. ID:", c.id)

		c.pubAddr = &net.UDPAddr{
			IP:   net.ParseIP(resp.PublicEndpoint.IpAddress),
			Port: int(resp.PublicEndpoint.Port),
		}
		c.registered <- true
	} else {
		log.Printf("Registration failed: %s", resp.Message)
		c.registered <- false
	}
}

func (c *Client) handleCommand(input string) {
	c.lobbyMutex.RLock()
	defer c.lobbyMutex.RUnlock()
	parts := strings.Split(input, " ")
	command := parts[0]

	switch command {
	case "create":
		if len(parts) != 3 {
			log.Println("Usage: create <lobby_name> <max_players>")
			return
		}
		maxPlayers, err := strconv.Atoi(parts[2])
		if err != nil {
			log.Println("Invalid max players")
			return
		}
		c.createLobby(parts[1], uint32(maxPlayers))
	case "join":
		if len(parts) != 2 {
			log.Println("Usage: join <lobby_id>")
			return
		}
		var lobbyToJoin *LobbyInfo
		for _, lobby := range c.AvailableLobbies {
			if lobby.ID == parts[1] {
				lobbyToJoin = lobby
				break
			}
		}
		if lobbyToJoin == nil {
			log.Println("Lobby not found")
			return
		}
		c.joinLobby(lobbyToJoin)
	case "leave":
		c.leaveLobby()
	case "list":
		c.sendLobbyListRequest()
	case "members":
		c.listLobbyMembers()
	case "connect":
		if len(parts) != 2 {
			log.Println("Usage: connect <client_index>")
			return
		}
		index, err := strconv.ParseUint(parts[1], 10, 32)
		if err != nil {
			log.Println("Invalid index:", err)
			return
		}
		c.connectToPeerByIndex(int(index))
	case "ping":
		if len(parts) != 2 {
			log.Println("Usage: ping <client_index>")
			return
		}
		index, err := strconv.ParseUint(parts[1], 10, 32)
		if err != nil {
			log.Println("Invalid index:", err)
			return
		}
		c.pingPeerByIndex(int(index))
	default:
		c.handleUnknownCommand(command)
	}
}

func (c *Client) listLobbyMembers() {
	if c.CurrentLobby == nil {
		log.Println("Not in a lobby")
		return
	}
	var memberNames []string
	for _, member := range c.CurrentLobby.Members {
		memberNames = append(memberNames, member.ClientId)
	}
	log.Println("Lobby members:", strings.Join(memberNames, ", "))
}

func (c *Client) connectToPeerByIndex(index int) {
	c.lobbyMutex.RLock()
	defer c.lobbyMutex.RUnlock()
	if c.CurrentLobby == nil || index < 0 || index >= len(c.CurrentLobby.Members) {
		log.Println("Invalid peer index")
		return
	}
	peerInfo := c.CurrentLobby.Members[index]
	peer := &Peer{
		id: peerInfo.ClientId,
		addr: &net.UDPAddr{
			IP:   net.ParseIP(peerInfo.PublicEndpoint.IpAddress),
			Port: int(peerInfo.PublicEndpoint.Port),
		},
	}
	c.connectToPeer(peer)
}

func (c *Client) pingPeerByIndex(index int) {
	c.lobbyMutex.RLock()
	defer c.lobbyMutex.RUnlock()
	if c.CurrentLobby == nil || index < 0 || index >= len(c.CurrentLobby.Members) {
		log.Println("Invalid peer index")
		return
	}
	peerInfo := c.CurrentLobby.Members[index]
	peer := &Peer{
		id: peerInfo.ClientId,
		addr: &net.UDPAddr{
			IP:   net.ParseIP(peerInfo.PublicEndpoint.IpAddress),
			Port: int(peerInfo.PublicEndpoint.Port),
		},
	}
	c.sendPing(peer)
}


func (c *Client) listLobbies() {
	c.sendLobbyListRequest()
}

func (c *Client) stopClient() {
	c.stopOnce.Do(func() {
		close(c.stop)
	})
}

func (c *Client) handleUnknownCommand(command string) {
	log.Printf("Unknown command: %s", command)
	c.printHelp()
}

func (c *Client) printHelp() {
	log.Println("Available commands:")
	log.Println("  list - list available lobbies")
	log.Println("  create <name> <max_players> - create a lobby")
	log.Println("  join <id> - join a lobby by id")
	log.Println("  leave - leave the current lobby")
	log.Println("  members - list members in the current lobby")
	log.Println("  ping <index> - ping a client by index")
	log.Println("  connect <index> - connect to a client by index")
	log.Println("  help - print this help message")
	log.Println("  exit - exit the client")
}

func main() {
	address := flag.String("a", "", "Server address")
	port := flag.String("p", "", "Port to use")

	flag.Parse()

	client := &Client{}

	if *address == "" || *port == "" {
		form := newFormModel()
		p := tea.NewProgram(form)
		if _, err := p.Run(); err != nil {
			log.Fatalf("could not run form: %s", err)
		}

		*address = form.inputs[0].Value()
		*port = form.inputs[1].Value()
	}

	if err := client.Init(*address, *port); err != nil {
		log.Fatalf("could not initialize client: %s", err)
	}
	client.Run()
}
