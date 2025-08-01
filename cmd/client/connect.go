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
	if _, ok := c.KnownPeers[peerToConnect.id]; ok {
		log.Println("Already know peer", peerToConnect.id)
		return
	}

	log.Printf("Requesting connection to peer %s", peerToConnect.id)
	peerToConnect.state = PENDING
	c.KnownPeers[peerToConnect.id] = peerToConnect

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
		delete(c.KnownPeers, peerToConnect.id)
	} else {
		log.Printf("Connection request for peer %s sent to server.", peerToConnect.id)
	}
}

// handleConnectResponse handles an incoming connection response from a peer.
func (c *Client) handleConnectResponse(resp *api.ConnectResponse, addr *net.UDPAddr) {
	if !addr.IP.Equal(c.srvAddr.IP) || addr.Port != c.srvAddr.Port {
		log.Printf("Received connect response from unexpected address: %s (expected %s)", addr, c.srvAddr)
		return
	}

	log.Printf("Received connect response for peer: %s", resp.ClientId)
	peer, ok := c.KnownPeers[resp.ClientId]
	if !ok {
		log.Printf("Received connect info for peer we are not connecting to: %s. Adding to known peers.", resp.ClientId)
		peer = &Peer{id: resp.ClientId}
		c.KnownPeers[resp.ClientId] = peer
	}

	peer.addr = &net.UDPAddr{IP: net.ParseIP(resp.PublicEndpoint.IpAddress), Port: int(resp.PublicEndpoint.Port)}
	peer.localAddr = &net.UDPAddr{IP: net.ParseIP(resp.LocalEndpoint.IpAddress), Port: int(resp.LocalEndpoint.Port)}
	peer.state = PENDING

	log.Printf("Starting hole punch for peer %s at %s (public) and %s (local)", peer.id, peer.addr, peer.localAddr)
	go c.holePunch(peer)
}

// handleConnectionEstablished handles a connection established message from a peer.
func (c *Client) handleConnectionEstablished(msg *api.ConnectionEstablished, addr *net.UDPAddr) {
	log.Println("ConnectionEstablished from peer:", msg.ClientId, "at address:", addr)
	peer, ok := c.KnownPeers[msg.ClientId]
	if !ok {
		log.Printf("Received ConnectionEstablished from unknown peer %s", msg.ClientId)
		return
	}

	if peer.state == CONNECTED {
		return // Already connected
	}

	peer.state = CONNECTED
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
	go c.verifyConnection(peer, addr)
}

// handleConnectRequest handles an incoming connection request from a peer.
func (c *Client) handleConnectRequest(req *api.ConnectRequest, addr *net.UDPAddr) {
	log.Printf("Received connection request from %s", req.SourceClientId)

	// Create a new peer entry if we don't know about this client
	peer, exists := c.KnownPeers[req.SourceClientId]
	if !exists {
		peer = &Peer{
			id:    req.SourceClientId,
			addr:  addr,
			state: PENDING,
		}
		c.KnownPeers[req.SourceClientId] = peer
	} else {
		peer.state = PENDING
	}

	// Update peer's local endpoint if provided
	if req.LocalEndpoint != nil {
		peer.localAddr = &net.UDPAddr{
			IP:   net.ParseIP(req.LocalEndpoint.IpAddress),
			Port: int(req.LocalEndpoint.Port),
		}
	}

	log.Printf("Starting hole punch for incoming connection from %s", req.SourceClientId)
	go c.holePunch(peer)
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
		if p, ok := c.KnownPeers[peer.id]; ok && p.state == CONNECTED {
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

	pingMsg := &api.Message{
		Content: &api.Message_DebugMessage{
			DebugMessage: &api.DebugMessage{Message: "PING"},
		},
	}
	pingBytes, _ := proto.Marshal(pingMsg)

	for {
		select {
		case <-ticker.C:
			if peer.state != CONNECTED {
				log.Printf("Peer %s disconnected, stopping verification", peer.id)
				return
			}

			start := time.Now()
			_, err := c.conn.WriteTo(pingBytes, addr)
			if err != nil {
				log.Printf("Failed to send ping to %s: %v", peer.id, err)
				peer.state = DISCONNECTED
				return
			}

			// Set a short timeout for ping response
			c.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			respBuf := make([]byte, 1024)
			_, respAddr, err := c.conn.ReadFromUDP(respBuf)
			if err != nil {
				log.Printf("No ping response from %s: %v", peer.id, err)
				continue
			}

			if respAddr.IP.Equal(addr.IP) && respAddr.Port == addr.Port {
				latency := time.Since(start)
				log.Printf("Ping to %s: %v", peer.id, latency)
			}
		case <-c.stop:
			return
		}
	}
}
