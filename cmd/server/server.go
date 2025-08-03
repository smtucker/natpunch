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

	"google.golang.org/protobuf/proto"

	api "natpunch/proto/gen/go"
	. "natpunch/pkg/models"
)

var bufferSize = 1024

// keepAliveTimeout is the duration after which a client is considered disconnected
const keepAliveTimeout = 5 * time.Second

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
		s.registerClient(content, addr)
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
	case *api.Message_LeaveLobbyRequest:
		log.Println("Received LeaveLobbyRequest:", content.LeaveLobbyRequest)
		s.handleLeaveLobbyRequest(content, addr)
	case *api.Message_PeerConnectionReady:
		log.Println("Received PeerConnectionReady:", content.PeerConnectionReady)
		s.handlePeerConnectionReady(content, addr)
	default:
		log.Println("Received unknown message type:", content)
	}
	return nil
}

func (s *Server) notifyLobbyMembersOfPeerLeft(lobby *LobbyInfo, peerLeftID string) {
	msg := &api.Message{
		Content: &api.Message_PeerLeft{
			PeerLeft: &api.PeerLeft{
				ClientId: peerLeftID,
			},
		},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal PeerLeft message: %v", err)
		return
	}

	for memberID, memberClient := range lobby.Members {
		_, err := s.conn.WriteToUDP(out, memberClient.Addr)
		if err != nil {
			log.Printf("Failed to send PeerLeft notification to %s: %v", memberID, err)
		}
	}
}

func (s *Server) sendErrorResponse(recipientAddr *net.UDPAddr, errorMessage string) {
	resp := &api.Error{
		Message: errorMessage,
	}

	s.sendResponse(recipientAddr, &api.Message{
		Content: &api.Message_Error{Error: resp},
	})
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

func (s *Server) sendResponse(addr *net.UDPAddr, msg *api.Message) {
	out, err := proto.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal message: %v", err)
		return
	}
	_, err = s.conn.WriteToUDP(out, addr)
	if err != nil {
		log.Printf("Failed to send message to %s: %v", addr, err)
	}
}

func main() {
		addr := flag.String("a", "0.0.0.0", "Address to bind to")
	port := flag.String("p", "8080", "Port to use")

	flag.Parse()

	srv := &Server{}
	srv.run(*addr, *port)
}

