package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"natpunch/pkg/api"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

// TODO: Make this configurable
var bufferSize int = 1024

// Struct to store client info
type ClientInfo struct {
	Addr      *net.UDPAddr
	Type      api.NatType
	KeepAlive time.Time
}

type Server struct {
	Clients map[uuid.UUID]*ClientInfo
	Addr    *net.UDPAddr
	conn    *net.UDPConn
	stop    chan bool
	mut     sync.RWMutex
	wg      sync.WaitGroup
}

func (s *Server) Run(addr string, port string) {
	s.mut.Lock()
	s.Clients = make(map[uuid.UUID]*ClientInfo)

	fullAddr := "127.0.0.1" + ":" + port
	log.Println("Starting server on:", fullAddr)
	Addr, err := net.ResolveUDPAddr("udp", fullAddr)
	if err != nil {
		panic(err)
	}

	s.conn, err = net.ListenUDP("udp", Addr)
	if err != nil {
		panic(err)
	}
	defer s.conn.Close()

	log.Println("Listening on:", s.conn.LocalAddr())

	s.wg = sync.WaitGroup{}
	s.stop = make(chan bool)

	s.mut.Unlock()

	s.wg.Add(1)
	go s.listener()

	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
	<-sigchan // Block until we recieve a signal
	log.Println("Received signal, shutting down...")
	close(s.stop)
	s.wg.Wait()
	log.Println("Shutdown complete")
}

func (s *Server) listener() {
	defer s.wg.Done()

	buf := make([]byte, bufferSize)
	for {
		s.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		select {
		case <-s.stop:
			log.Println("Shutting down listener")
			return
		default:
			n, addr, err := s.conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				log.Println(err)
				return
			}
			log.Println("Received:", n, "bytes from", addr)

			s.wg.Add(1)
			go func(data []byte, addr *net.UDPAddr) {
				defer s.wg.Done()
				if err := s.handleMessage(data, addr); err != nil {
					log.Println("Failed to handle message:", err)
				}
			}(buf[:n], addr)
		}
	}
}

func (s *Server) handleMessage(data []byte, addr *net.UDPAddr) error {
	msg := &api.Message{}
	if err := proto.Unmarshal(data, msg); err != nil {
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}

	switch content := msg.Content.(type) {
	case *api.Message_RegisterRequest:
		log.Println("Received RegisterRequest:", content.RegisterRequest)
		s.registerClient(*content, addr)
	case *api.Message_ClientListRequest:
		log.Println("Received ClientListRequest:", content.ClientListRequest)
	case *api.Message_ConnectRequest:
		log.Println("Received ConnectRequest:", content.ConnectRequest)
	case *api.Message_KeepAlive:
		log.Println("Received KeepAlive:", content.KeepAlive)
	default:
		log.Println("Received unknown message type:", content)
	}
	return nil
}

func (s *Server) registerClient(msg api.Message_RegisterRequest, addr *net.UDPAddr) {
	id := uuid.New()
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
		Success: true,
		Id:      id.String(),
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
