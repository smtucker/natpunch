package main

import (
	"fmt"
	"log"
	"net"
	"time"

	api "natpunch/proto/gen/go"
	. "natpunch/pkg/models"
)

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
		log.Printf("ConnectRequest from %s has mismatched address. Expected %s, got %s", sourceClient.Addr.String(), addr.String())
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

	s.sendResponse(recipientAddr, &api.Message{
		Content: &api.Message_ConnectionInstruction{ConnectionInstruction: resp},
	})
}

func (s *Server) handlePeerConnectionReady(msg *api.Message_PeerConnectionReady, addr *net.UDPAddr) {
	req := msg.PeerConnectionReady
	log.Printf("Handling PeerConnectionReady for lobby %s, new peer %s", req.LobbyId, req.NewPeerId)

	s.mut.Lock()
	defer s.mut.Unlock()

	lobby, lobbyExists := s.Lobbies[req.LobbyId]
	if !lobbyExists {
		log.Printf("Lobby %s not found for PeerConnectionReady", req.LobbyId)
		return
	}

	// Verify the message is from the host
	if _, ok := s.Clients[lobby.HostClientID]; !ok || !s.Clients[lobby.HostClientID].Addr.IP.Equal(addr.IP) || s.Clients[lobby.HostClientID].Addr.Port != addr.Port {
		log.Printf("PeerConnectionReady for lobby %s not from host. Expected %s, got %s", req.LobbyId, s.Clients[lobby.HostClientID].Addr.String(), addr.String())
		return
	}

	// Add the new peer to the lobby
	newPeer, peerExists := s.Clients[req.NewPeerId]
	if !peerExists {
		log.Printf("New peer %s not found for PeerConnectionReady", req.NewPeerId)
		return
	}
	lobby.Members[req.NewPeerId] = newPeer
	log.Printf("Client %s successfully joined lobby %s", req.NewPeerId, req.LobbyId)

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

	// Send JoinLobbySuccess to the newly joined client
	joinResp := &api.JoinLobbySuccess{
		Success:      true,
		LobbyId:      req.LobbyId,
		Message:      "Successfully joined lobby and connected to host",
		LobbyMembers: members,
	}
	s.sendResponse(newPeer.Addr, &api.Message{
		Content: &api.Message_JoinLobbySuccess{JoinLobbySuccess: joinResp},
	})

	// Notify all other lobby members of the new player
	s.notifyLobbyMembers(lobby, req.NewPeerId, "player_joined")

	// Trigger connections between the new player and all other existing members
	s.triggerLobbyConnections(lobby, req.NewPeerId)
}

func (s *Server) waitForPeerConnection(lobbyID string, newPeerID string) {
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()

	// This is a simplified example. In a real application, you'd use a more robust
	// mechanism to wait for the PeerConnectionReady message, probably involving channels.
	// For now, we'll just sleep and check. This is not ideal.
	// A better approach would be to have a pending connections map.
	// We will implement this if the current approach is not sufficient.

	// The `handlePeerConnectionReady` function will handle the actual logic.
	// This function's role is mostly to timeout the connection attempt.
	// We'll check periodically if the peer was added.

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout.C:
			s.mut.Lock()
			lobby, lobbyExists := s.Lobbies[lobbyID]
			if lobbyExists {
				if _, memberExists := lobby.Members[newPeerID]; !memberExists {
					log.Printf("Connection timeout for peer %s in lobby %s", newPeerID, lobbyID)
					// Notify the joining client that they failed to connect
					if client, ok := s.Clients[newPeerID]; ok {
						s.sendErrorResponse(client.Addr, "Failed to establish connection with lobby host")
					}
				}
			}
			s.mut.Unlock()
			return
		case <-ticker.C:
			s.mut.RLock()
			lobby, lobbyExists := s.Lobbies[lobbyID]
			if lobbyExists {
				if _, memberExists := lobby.Members[newPeerID]; memberExists {
					s.mut.RUnlock()
					return // Success!
				}
			}
			s.mut.RUnlock()
		}
	}
}
