package main

import (
	"log"
	"net"
	"time"

	"github.com/google/uuid"
	api "natpunch/proto/gen/go"
	. "natpunch/pkg/models"
)

// registerClient registers a client with the server generating
// a uuid and saving the client in the client list
func (s *Server) registerClient(msg *api.Message_RegisterRequest, addr *net.UDPAddr) {
	id := uuid.New().String()
	localAddr := &net.UDPAddr{
		IP:   net.ParseIP(msg.RegisterRequest.LocalEndpoint.IpAddress),
		Port: int(msg.RegisterRequest.LocalEndpoint.Port),
	}
	client := &ClientInfo{
		Addr:      addr, // This is the public endpoint of the client
		LocalAddr: localAddr,
		KeepAlive: time.Now(),
		Type:      api.NatType_UNKNOWN,
	}
	s.mut.Lock()
	s.Clients[id] = client
	s.mut.Unlock()
	log.Println("Registered client:", id, "at address:", addr)

	regResp := &api.RegisterResponse{
		Success:  true,
		ClientId: id,
		PublicEndpoint: &api.Endpoint{
			IpAddress: addr.IP.String(),
			Port:      uint32(addr.Port),
		},
	}

	s.sendResponse(addr, &api.Message{
		Content: &api.Message_RegisterResponse{RegisterResponse: regResp},
	})
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
