package main

import (
	"log"
	"net"
	"time"

	api "natpunch/proto/gen/go"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

// registerClient registers a client with the server generating
// a uuid and saving the client in the client list
func (s *Server) registerClient(msg api.Message_RegisterRequest, addr *net.UDPAddr) {
	id := uuid.New().String()
	localAddr := &net.UDPAddr{
		IP:   net.ParseIP(msg.RegisterRequest.LocalEndpoint.IpAddress),
		Port: int(msg.RegisterRequest.LocalEndpoint.Port),
	}
	client := ClientInfo{
		Addr:      addr, // This is the public endpoint of the client
		LocalAddr: localAddr,
		KeepAlive: time.Now(),
		Type:      api.NatType_UNKNOWN,
	}
	s.mut.Lock()
	s.Clients[id] = &client
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

	resp := api.Message{
		Content: &api.Message_RegisterResponse{RegisterResponse: regResp},
	}
	out, err := proto.Marshal(&resp)
	if err != nil {
		log.Println("Failed to marshal register response:", err)
	}
	_, err = s.conn.WriteToUDP(out, addr)
	if err != nil {
		log.Println("Failed to send register response:", err)
	}
}
