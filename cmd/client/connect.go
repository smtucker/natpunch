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
	addr    *net.UDPAddr
	natType api.NatType
	id      string
	state   PeerState
}

// connectToPeer initiates a connection to the specified peer.
func (c *Client) connectToPeer(peerToConnect *Peer) {
	if _, ok := c.KnownPeers[peerToConnect.id]; ok {
		log.Println("Already know peer", peerToConnect.id)
		return
	}

	peerToConnect.state = PENDING
	c.KnownPeers[peerToConnect.id] = peerToConnect
	ticker := time.NewTicker(KeepAliveInterval)
	defer ticker.Stop()

	ep := api.Endpoint{IpAddress: c.pubAddr.IP.String(), Port: uint32(c.pubAddr.Port)}
	msg := &api.ConnectRequest{
		SourceClientId:      c.id,
		DestinationClientId: peerToConnect.id,
		LocalEndpoint:       &ep,
	}

	for c.KnownPeers[peerToConnect.id].state == PENDING && msg.AttemptNumber < 10 { // Max connect attempts
		msg.AttemptNumber++
		fullMsg := &api.Message{
			Content: &api.Message_ConnectRequest{ConnectRequest: msg},
		}
		bytes, err := proto.Marshal(fullMsg)
		if err != nil {
			log.Println("Failed to marshal connect request:", err)
			continue
		}
		_, err = c.conn.WriteTo(bytes, peerToConnect.addr)
		if err != nil {
			log.Println("Failed to send connect request:", err)
		}
		log.Printf("sending connection request to %s, at %s, try #%d",
			peerToConnect.id, peerToConnect.addr.IP.String(), msg.AttemptNumber)
		<-ticker.C
	}
}

// handleConnectResponse handles an incoming connection response from a peer.
func (c *Client) handleConnectResponse(resp *api.ConnectResponse, addr *net.UDPAddr) {
	if peer, ok := c.KnownPeers[resp.ClientId]; ok {
		if !peer.addr.IP.Equal(addr.IP) {
			log.Println("Received ConnectResponse from different IP address:", resp.ClientId, "expected:", peer.addr.IP, "got:", addr.IP)
			return
		}
		peer.state = CONNECTED
		log.Println("ConnectionResponse from peer:", resp.ClientId, "at address:", addr)
		msg := &api.Message{
			Content: &api.Message_ConnectionEstablished{
				ConnectionEstablished: &api.ConnectionEstablished{ClientId: c.id},
			},
		}
		bytes, _ := proto.Marshal(msg)
		c.conn.WriteTo(bytes, addr)
	}
}

// handleConnectRequest handles an incoming connection request from a peer.
func (c *Client) handleConnectRequest(req *api.ConnectRequest, addr *net.UDPAddr) {
	log.Println("ConnectRequest from peer:", req.SourceClientId, "at address:", addr)
	msg := &api.Message{
		Content: &api.Message_ConnectResponse{
			ConnectResponse: &api.ConnectResponse{ClientId: c.id},
		},
	}
	bytes, _ := proto.Marshal(msg)
	c.conn.WriteTo(bytes, addr)
}

// handleConnectionEstablished handles a connection established message from a peer.
func (c *Client) handleConnectionEstablished(msg *api.ConnectionEstablished, addr *net.UDPAddr) {
	log.Println("ConnectionEstablished from peer:", msg.ClientId, "at address:", addr)
}
