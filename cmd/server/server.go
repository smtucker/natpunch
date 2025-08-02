package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	api "natpunch/proto/gen/go"

	"google.golang.org/protobuf/proto"
)

// TODO: Make this configurable
var bufferSize = 1024

// keepAliveTimeout is the duration after which a client is considered disconnected
const keepAliveTimeout = 5 * time.Second

// ClientInfo is the truct to store client info
type ClientInfo struct {
	Addr      *net.UDPAddr
	LocalAddr *net.UDPAddr
	Type      api.NatType
	KeepAlive time.Time
}

// LobbyInfo represents a game lobby
type LobbyInfo struct {
	ID           string
	Name         string
	HostClientID string
	MaxPlayers   uint32
	Members      map[string]*ClientInfo
	CreatedAt    time.Time
}

// Server is the main nat traversal middleman server
// create a an empty server and call it's Run method to
// initialize.
type Server struct {
	Clients map[string]*ClientInfo
	Lobbies map[string]*LobbyInfo
	Addr    *net.UDPAddr
	conn    *net.UDPConn
	stop    chan bool
	mut     sync.RWMutex
	wg      sync.WaitGroup
}

func (s *Server) run(addr string, port string) {
	s.mut.Lock()
	s.Clients = make(map[string]*ClientInfo)
	s.Lobbies = make(map[string]*LobbyInfo)

	fullAddr := addr + ":" + port
	log.Println("Starting server on:", fullAddr)
	Addr, err := net.ResolveUDPAddr("udp", fullAddr)
	if err != nil {
		panic(err)
	}

	s.conn, err = net.ListenUDP("udp", Addr)
	if err != nil {
		panic(err)
	}
	defer s.conn.Close()

	log.Println("Listening on:", s.conn.LocalAddr())

	s.wg = sync.WaitGroup{}
	s.stop = make(chan bool)

	s.mut.Unlock()

	s.wg.Add(1)
	go s.listener()

	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
	<-sigchan // Block until we recieve a signal
	log.Println("Received signal, shutting down...")
	close(s.stop)
	s.wg.Wait()
	log.Println("Shutdown complete")
}

func (s *Server) purgeClients() {
	now := time.Now()
	for id, client := range s.Clients {
		if now.Sub(client.KeepAlive) > keepAliveTimeout {
			log.Printf("Purging client %s due to inactivity", id)
			s.mut.Lock()
			delete(s.Clients, id)
			s.mut.Unlock()
		}
	}
}

func (s *Server) listener() {
	defer s.wg.Done()

	purgeTicker := time.NewTicker(10 * time.Second)
	defer purgeTicker.Stop()

	buf := make([]byte, bufferSize)
	for {
		s.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		select {
		case <-s.stop:
			log.Println("Shutting down listener")
			return
		case <-purgeTicker.C:
			go s.purgeClients()
		default:
			n, addr, err := s.conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				log.Println(err)
				return
			}
			log.Println("Received:", n, "bytes from", addr)

			s.wg.Add(1)
			go func(data []byte, addr *net.UDPAddr) {
				defer s.wg.Done()
				if err := s.handleMessage(data, addr); err != nil {
					log.Println("Failed to handle message:", err)
				}
			}(buf[:n], addr)
		}
	}
}

func (s *Server) handleMessage(data []byte, addr *net.UDPAddr) error {
	msg := &api.Message{}
	if err := proto.Unmarshal(data, msg); err != nil {
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}

	switch content := msg.Content.(type) {
	case *api.Message_RegisterRequest:
		log.Println("Received RegisterRequest:", content.RegisterRequest)
		s.registerClient(*content, addr)
	case *api.Message_ClientListRequest:
		log.Println("Received ClientListRequest:", content.ClientListRequest)
		s.handleClientListRequest(content, addr)
	case *api.Message_ConnectRequest:
		s.handleConnectRequest(content, addr)
	case *api.Message_KeepAlive:
		log.Println("Received KeepAlive:", content.KeepAlive)
		s.handleKeepAlive(content, addr)
	case *api.Message_CreateLobbyRequest:
		log.Println("Received CreateLobbyRequest:", content.CreateLobbyRequest)
		s.handleCreateLobbyRequest(content, addr)
	case *api.Message_JoinLobbyRequest:
		log.Println("Received JoinLobbyRequest:", content.JoinLobbyRequest)
		s.handleJoinLobbyRequest(content, addr)
	case *api.Message_LobbyListRequest:
		log.Println("Received LobbyListRequest:", content.LobbyListRequest)
		s.handleLobbyListRequest(content, addr)
	default:
		log.Println("Received unknown message type:", content)
	}
	return nil
}

func (s *Server) handleClientListRequest(msg *api.Message_ClientListRequest, addr *net.UDPAddr) {
	s.mut.RLock()
	defer s.mut.RUnlock()

	var clientList []*api.ClientInfo
	for id, client := range s.Clients {
		clientList = append(clientList, &api.ClientInfo{
			ClientId: id,
			PublicEndpoint: &api.Endpoint{
				IpAddress: client.Addr.IP.String(),
				Port:      uint32(client.Addr.Port),
			},
		})
	}

	resp := &api.ClientListResponse{
		Success: true,
		Clients: clientList,
	}

	data, err := proto.Marshal(&api.Message{
		Content: &api.Message_ClientListResponse{ClientListResponse: resp},
	})
	if err != nil {
		log.Println("Failed to marshal ClientListResponse:", err)
		return
	}

	if _, err := s.conn.WriteToUDP(data, addr); err != nil {
		log.Println("Failed to send ClientListResponse:", err)
	}
}

func (s *Server) handleConnectRequest(msg *api.Message_ConnectRequest, addr *net.UDPAddr) {
	req := msg.ConnectRequest
	log.Printf("Handling ConnectRequest from %s to %s", req.SourceClientId, req.DestinationClientId)

	s.mut.RLock()
	sourceClient, sourceOk := s.Clients[req.SourceClientId]
	destClient, destOk := s.Clients[req.DestinationClientId]
	s.mut.RUnlock()

	if !sourceOk {
		log.Printf("ConnectRequest from unknown client: %s", req.SourceClientId)
		return
	}

	if !sourceClient.Addr.IP.Equal(addr.IP) || sourceClient.Addr.Port != addr.Port {
		log.Printf("ConnectRequest from %s has mismatched address. Expected %s, got %s", req.SourceClientId, sourceClient.Addr.String(), addr.String())
		return
	}

	if !destOk {
		log.Printf("Destination client not found: %s", req.DestinationClientId)
		// Send an error response back to the source client
		s.sendErrorResponse(sourceClient.Addr, fmt.Sprintf("Destination client %s not found", req.DestinationClientId))
		return
	}

	// Send connection details of the destination to the source
	s.sendConnectionInstruction(sourceClient.Addr, req.DestinationClientId, destClient)

	// Send connection details of the source to the destination
	s.sendConnectionInstruction(destClient.Addr, req.SourceClientId, sourceClient)
	log.Printf("Exchanged endpoint information between %s and %s", req.SourceClientId, req.DestinationClientId)
}

func (s *Server) sendConnectionInstruction(recipientAddr *net.UDPAddr, peerID string, peerInfo *ClientInfo) {
	resp := &api.ConnectionInstruction{
		Accepted: true,
		ClientId: peerID,
		PublicEndpoint: &api.Endpoint{
			IpAddress: peerInfo.Addr.IP.String(),
			Port:      uint32(peerInfo.Addr.Port),
		},
		LocalEndpoint: &api.Endpoint{
			IpAddress: peerInfo.LocalAddr.IP.String(),
			Port:      uint32(peerInfo.LocalAddr.Port),
		},
	}

	msg := &api.Message{
		Content: &api.Message_ConnectionInstruction{ConnectionInstruction: resp},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal ConnectionInstruction for %s: %v", recipientAddr, err)
		return
	}

	_, err = s.conn.WriteToUDP(out, recipientAddr)
	if err != nil {
		log.Printf("Failed to send ConnectionInstruction to %s: %v", recipientAddr, err)
	}
}

func (s *Server) handleKeepAlive(msg *api.Message_KeepAlive, addr *net.UDPAddr) {
	ci, ok := s.Clients[msg.KeepAlive.ClientId]
	if !ok {
		log.Println("Received KeepAlive for unknown client:", msg.KeepAlive.ClientId)
		return
	}
	if !ci.Addr.IP.Equal(addr.IP) {
		log.Println(
			"Received KeepAlive from different IP address:",
			msg.KeepAlive.ClientId, "expected:", ci.Addr.IP,
			"got:", addr.IP)
		return
	}

	// If we got this far we have a valid client and should update the keep alive
	s.mut.Lock()
	ci.KeepAlive = time.Now()
	defer s.mut.Unlock()
}

func (s *Server) sendErrorResponse(recipientAddr *net.UDPAddr, errorMessage string) {
	resp := &api.Error{
		Message: errorMessage,
	}

	msg := &api.Message{
		Content: &api.Message_Error{Error: resp},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal Error response: %v", err)
		return
	}

	_, err = s.conn.WriteToUDP(out, recipientAddr)
	if err != nil {
		log.Printf("Failed to send Error response to %s: %v", recipientAddr, err)
	}
}

func (s *Server) handleCreateLobbyRequest(msg *api.Message_CreateLobbyRequest, addr *net.UDPAddr) {
	req := msg.CreateLobbyRequest

	// Verify client exists
	s.mut.RLock()
	client, exists := s.Clients[req.ClientId]
	s.mut.RUnlock()

	if !exists {
		s.sendErrorResponse(addr, "Client not registered")
		return
	}

	// Generate lobby ID
	lobbyID := fmt.Sprintf("lobby_%d", time.Now().UnixNano())

	// Create lobby
	lobby := &LobbyInfo{
		ID:           lobbyID,
		Name:         req.LobbyName,
		HostClientID: req.ClientId,
		MaxPlayers:   req.MaxPlayers,
		Members:      make(map[string]*ClientInfo),
		CreatedAt:    time.Now(),
	}
	lobby.Members[req.ClientId] = client

	// Add lobby to server
	s.mut.Lock()
	s.Lobbies[lobbyID] = lobby
	s.mut.Unlock()

	log.Printf("Created lobby %s: %s (host: %s)", lobbyID, req.LobbyName, req.ClientId)

	// Send response
	resp := &api.CreateLobbyResponse{
		Success: true,
		LobbyId: lobbyID,
		Message: "Lobby created successfully",
	}

	msgOut := &api.Message{
		Content: &api.Message_CreateLobbyResponse{CreateLobbyResponse: resp},
	}

	out, err := proto.Marshal(msgOut)
	if err != nil {
		log.Printf("Failed to marshal CreateLobbyResponse: %v", err)
		return
	}

	_, err = s.conn.WriteToUDP(out, addr)
	if err != nil {
		log.Printf("Failed to send CreateLobbyResponse: %v", err)
	}
}

func (s *Server) handleJoinLobbyRequest(msg *api.Message_JoinLobbyRequest, addr *net.UDPAddr) {
	req := msg.JoinLobbyRequest

	// Verify client exists
	s.mut.RLock()
	client, exists := s.Clients[req.ClientId]
	s.mut.RUnlock()

	if !exists {
		s.sendErrorResponse(addr, "Client not registered")
		return
	}

	// Find lobby
	s.mut.RLock()
	lobby, lobbyExists := s.Lobbies[req.LobbyId]
	s.mut.RUnlock()

	if !lobbyExists {
		s.sendErrorResponse(addr, "Lobby not found")
		return
	}

	// Check if lobby is full
	if uint32(len(lobby.Members)) >= lobby.MaxPlayers {
		s.sendErrorResponse(addr, "Lobby is full")
		return
	}

	// Add client to lobby
	s.mut.Lock()
	lobby.Members[req.ClientId] = client
	s.mut.Unlock()

	log.Printf("Client %s joined lobby %s", req.ClientId, req.LobbyId)

	// Prepare lobby members list for response
	var members []*api.ClientInfo
	for memberID, memberClient := range lobby.Members {
		members = append(members, &api.ClientInfo{
			ClientId: memberID,
			PublicEndpoint: &api.Endpoint{
				IpAddress: memberClient.Addr.IP.String(),
				Port:      uint32(memberClient.Addr.Port),
			},
		})
	}

	// Send response to joining client
	resp := &api.JoinLobbyResponse{
		Success:      true,
		LobbyId:      req.LobbyId,
		Message:      "Successfully joined lobby",
		LobbyMembers: members,
	}

	msgOut := &api.Message{
		Content: &api.Message_JoinLobbyResponse{JoinLobbyResponse: resp},
	}

	out, err := proto.Marshal(msgOut)
	if err != nil {
		log.Printf("Failed to marshal JoinLobbyResponse: %v", err)
		return
	}

	_, err = s.conn.WriteToUDP(out, addr)
	if err != nil {
		log.Printf("Failed to send JoinLobbyResponse: %v", err)
	}

	// Notify other lobby members about the new player
	s.notifyLobbyMembers(lobby, req.ClientId, "player_joined")

	// Trigger NAT punching between new player and existing members
	s.triggerLobbyConnections(lobby, req.ClientId)
}

func (s *Server) handleLobbyListRequest(msg *api.Message_LobbyListRequest, addr *net.UDPAddr) {
	req := msg.LobbyListRequest

	s.mut.RLock()
	defer s.mut.RUnlock()

	var lobbies []*api.LobbyInfo
	for lobbyID, lobby := range s.Lobbies {
		// Convert lobby members to API format
		var members []*api.ClientInfo
		for memberID, memberClient := range lobby.Members {
			members = append(members, &api.ClientInfo{
				ClientId: memberID,
				PublicEndpoint: &api.Endpoint{
					IpAddress: memberClient.Addr.IP.String(),
					Port:      uint32(memberClient.Addr.Port),
				},
			})
		}

		lobbies = append(lobbies, &api.LobbyInfo{
			LobbyId:        lobbyID,
			LobbyName:      lobby.Name,
			HostClientId:   lobby.HostClientID,
			CurrentPlayers: uint32(len(lobby.Members)),
			MaxPlayers:     lobby.MaxPlayers,
			Members:        members,
		})
	}

	resp := &api.LobbyListResponse{
		Success:   true,
		RequestId: req.RequestId,
		Lobbies:   lobbies,
		Message:   "Lobby list retrieved successfully",
	}

	msgOut := &api.Message{
		Content: &api.Message_LobbyListResponse{LobbyListResponse: resp},
	}

	out, err := proto.Marshal(msgOut)
	if err != nil {
		log.Printf("Failed to marshal LobbyListResponse: %v", err)
		return
	}

	_, err = s.conn.WriteToUDP(out, addr)
	if err != nil {
		log.Printf("Failed to send LobbyListResponse: %v", err)
	}
}

func (s *Server) notifyLobbyMembers(lobby *LobbyInfo, newPlayerID string, updateType string) {
	// Convert lobby to API format
	var members []*api.ClientInfo
	for memberID, memberClient := range lobby.Members {
		members = append(members, &api.ClientInfo{
			ClientId: memberID,
			PublicEndpoint: &api.Endpoint{
				IpAddress: memberClient.Addr.IP.String(),
				Port:      uint32(memberClient.Addr.Port),
			},
		})
	}

	lobbyInfo := &api.LobbyInfo{
		LobbyId:        lobby.ID,
		LobbyName:      lobby.Name,
		HostClientId:   lobby.HostClientID,
		CurrentPlayers: uint32(len(lobby.Members)),
		MaxPlayers:     lobby.MaxPlayers,
		Members:        members,
	}

	update := &api.LobbyUpdate{
		LobbyId:    lobby.ID,
		LobbyInfo:  lobbyInfo,
		UpdateType: updateType,
	}

	msg := &api.Message{
		Content: &api.Message_LobbyUpdate{LobbyUpdate: update},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal LobbyUpdate: %v", err)
		return
	}

	// Send update to all lobby members except the new player
	for memberID, memberClient := range lobby.Members {
		if memberID != newPlayerID {
			_, err = s.conn.WriteToUDP(out, memberClient.Addr)
			if err != nil {
				log.Printf("Failed to send LobbyUpdate to %s: %v", memberID, err)
			}
		}
	}
}

func (s *Server) triggerLobbyConnections(lobby *LobbyInfo, newPlayerID string) {
	// Get the new player's client info
	newPlayer, exists := lobby.Members[newPlayerID]
	if !exists {
		log.Printf("New player %s not found in lobby %s", newPlayerID, lobby.ID)
		return
	}

	// Trigger connections between new player and all existing members
	for memberID, memberClient := range lobby.Members {
		if memberID != newPlayerID {
			log.Printf("Triggering connection between %s and %s in lobby %s", newPlayerID, memberID, lobby.ID)

			// Send connection instruction to new player for existing member
			s.sendConnectionInstruction(newPlayer.Addr, memberID, memberClient)

			// Send connection instruction to existing member for new player
			s.sendConnectionInstruction(memberClient.Addr, newPlayerID, newPlayer)
		}
	}
}

func main() {
	addr := flag.String("a", "127.0.0.1", "Address to bind to")
	port := flag.String("p", "8080", "Port to use")

	flag.Parse()

	srv := &Server{}
	srv.run(*addr, *port)
}
