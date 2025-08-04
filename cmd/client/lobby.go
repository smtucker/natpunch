package main

import (
	"fmt"
	"log"
	"net"

	api "natpunch/proto/gen/go"
)

func (c *Client) sendLobbyListRequest() {
	req := &api.LobbyListRequest{
		RequestId: fmt.Sprintf("req_%d", c.id),
	}
	msg := &api.Message{
		Content: &api.Message_LobbyListRequest{LobbyListRequest: req},
	}

	c.send(msg)
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

	c.send(msg)
}

func (c *Client) joinLobby(lobby *LobbyInfo) {
	req := &api.JoinLobbyRequest{
		LobbyId:  lobby.ID,
		ClientId: c.id,
	}
	msg := &api.Message{
		Content: &api.Message_JoinLobbyRequest{JoinLobbyRequest: req},
	}

	c.send(msg)
}

func (c *Client) leaveLobby() {
	c.lobbyMutex.Lock()
	defer c.lobbyMutex.Unlock()
	if c.CurrentLobby == nil {
		log.Println("Not in a lobby")
		return
	}

	req := &api.LeaveLobbyRequest{
		LobbyId:  c.CurrentLobby.ID,
		ClientId: c.id,
	}
	msg := &api.Message{
		Content: &api.Message_LeaveLobbyRequest{LeaveLobbyRequest: req},
	}

	c.send(msg)
}

func (c *Client) handleCreateLobbyResponse(resp *api.CreateLobbyResponse) {
	if resp.Success {
		log.Printf("✅ Lobby created successfully! Lobby ID: %s", resp.LobbyId)
		c.lobbyMutex.Lock()
		defer c.lobbyMutex.Unlock()
		c.CurrentLobby = &LobbyInfo{
			ID:           resp.LobbyId,
			Name:         resp.LobbyName,
			HostClientID: c.id,
			Members: []*api.ClientInfo{
				{
					ClientId: c.id,
					PublicEndpoint: &api.Endpoint{
						IpAddress: c.pubAddr.IP.String(),
						Port:      uint32(c.pubAddr.Port),
					},
				},
			},
			CurrentPlayers: 1,
			Peers:          make(map[string]*Peer),
		}
	} else {
		log.Printf("❌ Failed to create lobby: %s\n", resp.Message)
	}
}

func (c *Client) handleJoinLobbyResponse(resp *api.JoinLobbyResponse) {
	if resp.Success {
		log.Printf("Attempting to join lobby %s...\n", resp.LobbyId)
		// Connection to host will be initiated by the server.
		// Peer list will be populated upon successful connection.
	} else {
		log.Printf("❌ Failed to join lobby: %s\n", resp.Message)
	}
}

func (c *Client) handleJoinLobbySuccess(resp *api.JoinLobbySuccess) {
	c.lobbyMutex.Lock()
	defer c.lobbyMutex.Unlock()
	if resp.Success {
		log.Printf("✅ Successfully joined lobby %s!\n", resp.LobbyId)
		log.Printf("📋 Lobby members (%d):\n", len(resp.LobbyMembers))
		for i, member := range resp.LobbyMembers {
			log.Printf("  %d - %s: %s:%d\n", i, member.ClientId, member.PublicEndpoint.IpAddress, member.PublicEndpoint.Port)
		}

		// Convert to local LobbyInfo
		c.CurrentLobby = &LobbyInfo{
			ID:             resp.LobbyId,
			Members:        resp.LobbyMembers,
			CurrentPlayers: uint32(len(resp.LobbyMembers)),
			Peers:          make(map[string]*Peer),
		}
		for _, member := range resp.LobbyMembers {
			if member.ClientId != c.id {
				c.CurrentLobby.Peers[member.ClientId] = &Peer{
					id:    member.ClientId,
					addr:  &net.UDPAddr{IP: net.ParseIP(member.PublicEndpoint.IpAddress), Port: int(member.PublicEndpoint.Port)},
					state: DISCONNECTED,
				}
			}
		}
	} else {
		log.Printf("❌ Failed to join lobby: %s\n", resp.Message)
	}
}

func (c *Client) handleLobbyListResponse(resp *api.LobbyListResponse) {
	c.lobbyMutex.Lock()
	defer c.lobbyMutex.Unlock()
	if resp.Success {
		log.Println("📋 Available Lobbies:")
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
			log.Printf("%d - %s (Host: %s, Players: %d/%d)\n",
				i, lobby.LobbyName, lobby.HostClientId, lobby.CurrentPlayers, lobby.MaxPlayers)
		}
	} else {
		log.Printf("❌ Failed to get lobby list: %s\n", resp.Message)
	}
}

func (c *Client) handleLobbyUpdate(update *api.LobbyUpdate) {
	c.lobbyMutex.Lock()
	defer c.lobbyMutex.Unlock()
	log.Printf("🔄 Lobby update for %s: %s\n", update.LobbyId, update.UpdateType)

	// Update current lobby if it matches
	if c.CurrentLobby != nil && c.CurrentLobby.ID == update.LobbyId {
		c.CurrentLobby.Name = update.LobbyInfo.LobbyName
		c.CurrentLobby.HostClientID = update.LobbyInfo.HostClientId
		c.CurrentLobby.CurrentPlayers = update.LobbyInfo.CurrentPlayers
		c.CurrentLobby.MaxPlayers = update.LobbyInfo.MaxPlayers
		c.CurrentLobby.Members = update.LobbyInfo.Members

		// Update the Peers map
		for _, member := range update.LobbyInfo.Members {
			if _, ok := c.CurrentLobby.Peers[member.ClientId]; !ok && member.ClientId != c.id {
				c.CurrentLobby.Peers[member.ClientId] = &Peer{
					id:    member.ClientId,
					addr:  &net.UDPAddr{IP: net.ParseIP(member.PublicEndpoint.IpAddress), Port: int(member.PublicEndpoint.Port)},
					state: DISCONNECTED,
				}
			}
		}

		log.Printf("📋 Updated lobby members (%d):\n", len(update.LobbyInfo.Members))
		for i, member := range update.LobbyInfo.Members {
			log.Printf("  %d - %s: %s:%d\n", i, member.ClientId, member.PublicEndpoint.IpAddress, member.PublicEndpoint.Port)
		}
	}
}

func (c *Client) handleLeaveLobbyResponse(resp *api.LeaveLobbyResponse) {
	if resp.Success {
		c.lobbyMutex.Lock()
		defer c.lobbyMutex.Unlock()
		log.Printf("✅ Left lobby successfully!\n")
		c.CurrentLobby = nil
	} else {
		log.Printf("❌ Failed to leave lobby: %s\n", resp.Message)
	}
}

func (c *Client) handlePeerLeft(resp *api.PeerLeft) {
	c.lobbyMutex.Lock()
	defer c.lobbyMutex.Unlock()
	if c.CurrentLobby == nil {
		return
	}
	var newMembers []*api.ClientInfo
	for _, member := range c.CurrentLobby.Members {
		if member.ClientId != resp.ClientId {
			newMembers = append(newMembers, member)
		}
	}
	c.CurrentLobby.Members = newMembers
	c.CurrentLobby.CurrentPlayers = uint32(len(newMembers))
	delete(c.CurrentLobby.Peers, resp.ClientId)
	log.Printf("Peer %s has left the lobby.\n", resp.ClientId)
}
