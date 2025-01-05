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

	api "natpunch/proto/gen/go"

	"google.golang.org/protobuf/proto"
)

// TODO: Make this configurable
var bufferSize int = 1024

// keepAliveTimeout is the duration after which a client is considered disconnected
const keepAliveTimeout = 5 * time.Second

// Struct to store client info
type ClientInfo struct {
	Addr      *net.UDPAddr
	Type      api.NatType
	KeepAlive time.Time
}

type Server struct {
	Clients map[string]*ClientInfo
	Addr    *net.UDPAddr
	conn    *net.UDPConn
	stop    chan bool
	mut     sync.RWMutex
	wg      sync.WaitGroup
}

func (s *Server) run(port string) {
	s.mut.Lock()
	s.Clients = make(map[string]*ClientInfo)

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
	case *api.Message_ConnectRequest:
		log.Println("Received ConnectRequest:", content.ConnectRequest)
	case *api.Message_KeepAlive:
		log.Println("Received KeepAlive:", content.KeepAlive)
		s.handleKeepAlive(content, addr)
	default:
		log.Println("Received unknown message type:", content)
	}
	return nil
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

func main() {
	port := flag.String("p", "8080", "Port to use")

	flag.Parse()

	srv := &Server{}
	srv.run(*port)
}
