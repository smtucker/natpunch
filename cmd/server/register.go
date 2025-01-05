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
	Client := ClientInfo{
		Addr:      addr,
		KeepAlive: time.Now(),
		Type:      api.NatType_UNKNOWN,
	}
	s.mut.Lock()
	s.Clients[id] = &Client
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
