package main

import (
	"fmt"
	"log"
	"net"
	"time"

	api "natpunch/proto/gen/go"
	. "natpunch/pkg/models"
)

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
		Success:   true,
		LobbyId:   lobbyID,
		LobbyName: req.LobbyName,
		Message:   "Lobby created successfully",
	}

	s.sendResponse(addr, &api.Message{
		Content: &api.Message_CreateLobbyResponse{CreateLobbyResponse: resp},
	})
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

	// Trigger connection between new client and host
	hostClient, hostExists := s.Clients[lobby.HostClientID]
	if !hostExists {
		s.sendErrorResponse(addr, "Lobby host not found")
		return
	}

	// Send a preliminary JoinLobbyResponse to the joining client to indicate the process has started
	preliminaryResp := &api.JoinLobbyResponse{
		Success: true,
		LobbyId: req.LobbyId,
		Message: "Attempting to connect to lobby host...",
	}
	s.sendResponse(addr, &api.Message{
		Content: &api.Message_JoinLobbyResponse{JoinLobbyResponse: preliminaryResp},
	})

	log.Printf("Triggering connection between new client %s and host %s for lobby %s", req.ClientId, lobby.HostClientID, req.LobbyId)
	s.sendConnectionInstruction(client.Addr, lobby.HostClientID, hostClient)
	s.sendConnectionInstruction(hostClient.Addr, req.ClientId, client)

	// Wait for PeerConnectionReady from host
	go s.waitForPeerConnection(req.LobbyId, req.ClientId)
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

	s.sendResponse(addr, &api.Message{
		Content: &api.Message_LobbyListResponse{LobbyListResponse: resp},
	})
}

func (s *Server) handleLeaveLobbyRequest(msg *api.Message_LeaveLobbyRequest, addr *net.UDPAddr) {
	req := msg.LeaveLobbyRequest

	// Find lobby
	s.mut.Lock()
	lobby, lobbyExists := s.Lobbies[req.LobbyId]
	if !lobbyExists {
		s.mut.Unlock()
		s.sendErrorResponse(addr, "Lobby not found")
		return
	}

	// Remove client from lobby
	if _, ok := lobby.Members[req.ClientId]; ok {
		delete(lobby.Members, req.ClientId)
		log.Printf("Client %s left lobby %s", req.ClientId, req.LobbyId)
	} else {
		s.mut.Unlock()
		s.sendErrorResponse(addr, "Client not in lobby")
		return
	}

	// Notify remaining members
	s.notifyLobbyMembersOfPeerLeft(lobby, req.ClientId)

	// If no members are left, delete the lobby
	if len(lobby.Members) == 0 {
		delete(s.Lobbies, req.LobbyId)
		log.Printf("Lobby %s is empty and has been deleted", req.LobbyId)
	} else if lobby.HostClientID == req.ClientId {
		// If the host left, assign a new host
		for newHostID := range lobby.Members {
			lobby.HostClientID = newHostID
			log.Printf("Lobby %s host changed to %s", req.LobbyId, newHostID)
			break
		}
	}
	s.mut.Unlock()

	// Send success response
	resp := &api.LeaveLobbyResponse{
		Success: true,
		Message: "Successfully left lobby",
	}

	s.sendResponse(addr, &api.Message{
		Content: &api.Message_LeaveLobbyResponse{LeaveLobbyResponse: resp},
	})
}
