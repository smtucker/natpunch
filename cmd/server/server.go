package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	api "natpunch/proto"

	"google.golang.org/protobuf/proto"
)

// TODO: Make this configurable
var bufferSize = 1024

// keepAliveTimeout is the duration after which a client is considered disconnected
const keepAliveTimeout = 5 * time.Second

// ClientInfo is the truct to store client info
type ClientInfo struct {
	Addr      *net.UDPAddr
	LocalAddr *net.UDPAddr
	Type      api.NatType
	KeepAlive time.Time
}

// Server is the main nat traversal middleman server
// create a an empty server and call it's Run method to
// initialize.
type Server struct {
	Clients map[string]*ClientInfo
	Addr    *net.UDPAddr
	conn    *net.UDPConn
	stop    chan bool
	mut     sync.RWMutex
	wg      sync.WaitGroup
}

func (s *Server) run(addr string, port string) {
	s.mut.Lock()
	s.Clients = make(map[string]*ClientInfo)

	fullAddr := addr + ":" + port
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

func (s *Server) listener() {
	defer s.wg.Done()

	purgeTicker := time.NewTicker(10 * time.Second)
	defer purgeTicker.Stop()

	buf := make([]byte, bufferSize)
	for {
		s.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		select {
		case <-s.stop:
			log.Println("Shutting down listener")
			return
		case <-purgeTicker.C:
			go s.purgeClients()
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
		s.handleClientListRequest(content, addr)
	case *api.Message_ConnectRequest:
		s.handleConnectRequest(content, addr)
	case *api.Message_KeepAlive:
		log.Println("Received KeepAlive:", content.KeepAlive)
		s.handleKeepAlive(content, addr)
	default:
		log.Println("Received unknown message type:", content)
	}
	return nil
}

func (s *Server) handleClientListRequest(msg *api.Message_ClientListRequest, addr *net.UDPAddr) {
	s.mut.RLock()
	defer s.mut.RUnlock()

	var clientList []*api.ClientInfo
	for id, client := range s.Clients {
		clientList = append(clientList, &api.ClientInfo{
			ClientId: id,
			PublicEndpoint: &api.Endpoint{
				IpAddress: client.Addr.IP.String(),
				Port:      uint32(client.Addr.Port),
			},
		})
	}

	resp := &api.ClientListResponse{
		Success: true,
		Clients: clientList,
	}

	data, err := proto.Marshal(&api.Message{
		Content: &api.Message_ClientListResponse{ClientListResponse: resp},
	})
	if err != nil {
		log.Println("Failed to marshal ClientListResponse:", err)
		return
	}

	if _, err := s.conn.WriteToUDP(data, addr); err != nil {
		log.Println("Failed to send ClientListResponse:", err)
	}
}

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
		log.Printf("ConnectRequest from %s has mismatched address. Expected %s, got %s", req.SourceClientId, sourceClient.Addr.String(), addr.String())
		return
	}

	if !destOk {
		log.Printf("Destination client not found: %s", req.DestinationClientId)
		// Send an error response back to the source client
		s.sendErrorResponse(sourceClient.Addr, fmt.Sprintf("Destination client %s not found", req.DestinationClientId))
		return
	}

	// Send connection details of the destination to the source
	s.sendConnectResponse(sourceClient.Addr, req.DestinationClientId, destClient)

	// Send connection details of the source to the destination
	s.sendConnectResponse(destClient.Addr, req.SourceClientId, sourceClient)
	log.Printf("Exchanged endpoint information between %s and %s", req.SourceClientId, req.DestinationClientId)
}

func (s *Server) sendConnectResponse(recipientAddr *net.UDPAddr, peerID string, peerInfo *ClientInfo) {
	resp := &api.ConnectResponse{
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

	msg := &api.Message{
		Content: &api.Message_ConnectResponse{ConnectResponse: resp},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal ConnectResponse for %s: %v", recipientAddr, err)
		return
	}

	_, err = s.conn.WriteToUDP(out, recipientAddr)
	if err != nil {
		log.Printf("Failed to send ConnectResponse to %s: %v", recipientAddr, err)
	}
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

func (s *Server) sendErrorResponse(recipientAddr *net.UDPAddr, errorMessage string) {
	resp := &api.Error{
		Message: errorMessage,
	}

	msg := &api.Message{
		Content: &api.Message_Error{Error: resp},
	}

	out, err := proto.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal Error response: %v", err)
		return
	}

	_, err = s.conn.WriteToUDP(out, recipientAddr)
	if err != nil {
		log.Printf("Failed to send Error response to %s: %v", recipientAddr, err)
	}
}

func main() {
	addr := flag.String("a", "127.0.0.1", "Address to bind to")
	port := flag.String("p", "8080", "Port to use")

	flag.Parse()

	srv := &Server{}
	srv.run(*addr, *port)
}
