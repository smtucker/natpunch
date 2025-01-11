package main

import (
	"log"
	"net"
	"time"

	api "natpunch/proto/gen/go"

	"google.golang.org/protobuf/proto"
)

type peerState int

const (
	DISCONNECTED peerState = iota
	PENDING
	CONNECTED
)

type peer struct {
	addr    *net.UDPAddr
	natType api.NatType
	id      string
	state   peerState
}

func (c *Client) peerConnect(newPeer *peer) {
	if knownPeer, ok := c.KnownPeers[newPeer.id]; ok {
		log.Println("Already know peer", knownPeer.id)
		return
	}
	newPeer.state = PENDING
	c.KnownPeers[newPeer.id] = newPeer
	ticker := time.NewTicker(2 * time.Second)
	ep := api.Endpoint{IpAddress: c.pubAddr.IP.String(), Port: uint32(c.pubAddr.Port)}
	msg := &api.ConnectRequest{
		SourceClientId:      c.id,
		DestinationClientId: newPeer.id,
		LocalEndpoint:       &ep,
	}
	for c.KnownPeers[newPeer.id].state == PENDING && msg.AttemptNumber < 10 {
		msg.AttemptNumber++
		fullMsg := &api.Message{
			Content: &api.Message_ConnectRequest{ConnectRequest: msg},
		}
		bytes, err := proto.Marshal(fullMsg)
		if err != nil {
			log.Println("Failed to marshal connect request:", err)
			continue
		}
		_, err = c.conn.WriteTo(bytes, newPeer.addr)
		if err != nil {
			log.Println("Failed to send connect request:", err)
		}
		<-ticker.C
	}
}

func (c *Client) handleConnectResponse(resp *api.ConnectResponse, addr *net.UDPAddr) {
	// TODO: Implement spoofing checking
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
		} //&api.ConnectionEstablished{ClientId: c.id}
		bytes, _ := proto.Marshal(msg)
		c.conn.WriteTo(bytes, addr)
	}
}

func (c *Client) handleConnectRequest(req *api.ConnectRequest, addr *net.UDPAddr) {
	// TODO: Handle connect request
	log.Println("ConnectRequest from peer:", req.SourceClientId, "at address:", addr)
	msg := &api.Message{
		Content: &api.Message_ConnectResponse{
			ConnectResponse: &api.ConnectResponse{ClientId: c.id},
		},
	} //&api.ConnectResponse{ClientId: c.id}
	bytes, _ := proto.Marshal(msg)
	c.conn.WriteTo(bytes, addr)
}

func (c *Client) handleConnectionEstablished(msg *api.ConnectionEstablished, addr *net.UDPAddr) {
	// TODO: Handle connection established
	log.Println("ConnectionEstablished from peer:", msg.ClientId, "at address:", addr)
}
