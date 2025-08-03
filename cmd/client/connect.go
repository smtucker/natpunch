package main

import (
	"log"
	"net"
	"time"

	api "natpunch/proto/gen/go"

	"google.golang.org/protobuf/proto"
)

// PeerState represents the connection state of a peer.
type PeerState int

// Constants for peer connection states.
const (
	DISCONNECTED PeerState = iota
	PENDING
	CONNECTED
)

// Peer represents a remote client.
type Peer struct {
	addr      *net.UDPAddr
	localAddr *net.UDPAddr
	natType   api.NatType
	id        string
	state     PeerState
}

// connectToPeer initiates a connection to the specified peer.
func (c *Client) connectToPeer(peerToConnect *Peer) {
	log.Printf("Requesting connection to peer %s", peerToConnect.id)

	ep := api.Endpoint{IpAddress: c.localAddr.IP.String(), Port: uint32(c.localAddr.Port)}
	msg := &api.ConnectRequest{
		SourceClientId:      c.id,
		DestinationClientId: peerToConnect.id,
		LocalEndpoint:       &ep,
	}

	fullMsg := &api.Message{
		Content: &api.Message_ConnectRequest{ConnectRequest: msg},
	}
	bytes, err := proto.Marshal(fullMsg)
	if err != nil {
		log.Println("Failed to marshal connect request:", err)
		return
	}
	_, err = c.conn.WriteTo(bytes, c.srvAddr)
	if err != nil {
		log.Println("Failed to send connect request:", err)
	} else {
		log.Printf("Connection request for peer %s sent to server.", peerToConnect.id)
	}
}

// handleConnectionInstruction handles an incoming connection instruction from the server.
func (c *Client) handleConnectionInstruction(resp *api.ConnectionInstruction, addr *net.UDPAddr) {
	if !addr.IP.Equal(c.srvAddr.IP) || addr.Port != c.srvAddr.Port {
		log.Printf("Received connection instruction from unexpected address: %s (expected %s)", addr, c.srvAddr)
		return
	}

	if c.CurrentLobby == nil {
		log.Println("Received connection instruction while not in a lobby, ignoring.")
		return
	}

	c.lobbyMutex.Lock()
	defer c.lobbyMutex.Unlock()

	peer := &Peer{
		id:        resp.ClientId,
		addr:      &net.UDPAddr{IP: net.ParseIP(resp.PublicEndpoint.IpAddress), Port: int(resp.PublicEndpoint.Port)},
		localAddr: &net.UDPAddr{IP: net.ParseIP(resp.LocalEndpoint.IpAddress), Port: int(resp.LocalEndpoint.Port)},
		state:     PENDING,
	}

	if _, ok := c.CurrentLobby.Peers[peer.id]; !ok {
		c.CurrentLobby.Peers[peer.id] = peer
	}


	log.Printf("Starting hole punch for peer %s at %s (public) and %s (local)", peer.id, peer.addr, peer.localAddr)
	go c.holePunch(peer)
}

// handleConnectionEstablished handles a connection established message from a peer.
func (c *Client) handleConnectionEstablished(msg *api.ConnectionEstablished, addr *net.UDPAddr) {
	log.Println("ConnectionEstablished from peer:", msg.ClientId, "at address:", addr)
	c.lobbyMutex.Lock()
	defer c.lobbyMutex.Unlock()
	if peer, ok := c.CurrentLobby.Peers[msg.ClientId]; ok {
		if peer.state == CONNECTED {
			return // Already connected
		}
		peer.state = CONNECTED
		if c.isLobbyHost() {
			c.sendPeerConnectionReady(c.CurrentLobby.ID, msg.ClientId)
		}
	} else {
		log.Printf("Received ConnectionEstablished from unknown peer %s", msg.ClientId)
		return
	}
	log.Printf("Connection to peer %s established! Remote address is %s", msg.ClientId, addr)

	// Send a confirmation back to the same address we received it from
	reply := &api.Message{
		Content: &api.Message_ConnectionEstablished{
			ConnectionEstablished: &api.ConnectionEstablished{ClientId: c.id, Message: "Connection Confirmed!"},
		},
	}
	bytes, _ := proto.Marshal(reply)
	c.conn.WriteTo(bytes, addr)

	// Start connection verification
	go c.verifyConnection(c.CurrentLobby.Peers[msg.ClientId], addr)
}

func (c *Client) sendPeerConnectionReady(lobbyID, peerID string) {
	log.Printf("Informing server that connection with peer %s is ready.", peerID)
	req := &api.PeerConnectionReady{
		LobbyId:   lobbyID,
		NewPeerId: peerID,
	}
	msg := &api.Message{
		Content: &api.Message_PeerConnectionReady{PeerConnectionReady: req},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal PeerConnectionReady: %v", err)
		return
	}

	_, err = c.conn.WriteTo(out, c.srvAddr)
	if err != nil {
		log.Printf("Failed to send PeerConnectionReady: %v", err)
	}
}


// holePunch performs NAT traversal by sending packets to the peer's public and local addresses.
func (c *Client) holePunch(peer *Peer) {
	log.Printf("Attempting to hole punch to peer %s", peer.id)
	ticker := time.NewTicker(500 * time.Millisecond) // send packets every 500ms
	defer ticker.Stop()

	// Message to send for hole punching
	msg := &api.Message{
		Content: &api.Message_ConnectionEstablished{
			ConnectionEstablished: &api.ConnectionEstablished{ClientId: c.id, Message: "Hole Punch"},
		},
	}
	bytes, err := proto.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal hole punch message: %v", err)
		return
	}

	// Attempt to punch for 10 seconds
	timeout := time.After(10 * time.Second)

	for {
		// Check if we are already connected
		if p, ok := c.CurrentLobby.Peers[peer.id]; ok && p.state == CONNECTED {
			log.Printf("Hole punch to %s successful", peer.id)
			return
		}

		select {
		case <-timeout:
			log.Printf("Hole punch to %s timed out", peer.id)
			// Maybe set peer state back to disconnected
			peer.state = DISCONNECTED
			return
		case <-ticker.C:
			// Send to public address
			_, err := c.conn.WriteTo(bytes, peer.addr)
			if err != nil {
				log.Printf("Failed to send hole punch to public addr %s: %v", peer.addr, err)
			} else {
				log.Printf("Sent hole punch packet to public %s", peer.addr)
			}

			// Send to local address if it's different
			if peer.localAddr != nil && (peer.localAddr.IP.String() != peer.addr.IP.String() || peer.localAddr.Port != peer.addr.Port) {
				_, err = c.conn.WriteTo(bytes, peer.localAddr)
				if err != nil {
					log.Printf("Failed to send hole punch to local addr %s: %v", peer.localAddr, err)
				} else {
					log.Printf("Sent hole punch packet to local %s", peer.localAddr)
				}
			}
		}
	}
}

// verifyConnection sends periodic ping messages to verify the connection is still alive
func (c *Client) verifyConnection(peer *Peer, addr *net.UDPAddr) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if peer.state != CONNECTED {
				log.Printf("Peer %s disconnected, stopping verification", peer.id)
				return
			}
			c.sendPing(peer)
		case <-c.stop:
			return
		}
	}
}
