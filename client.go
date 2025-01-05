package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"natpunch/pkg/api"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

type Client struct {
	conn    *net.UDPConn
	srvAddr *net.UDPAddr
	pubAddr *net.UDPAddr
	stop    chan struct{}
	wg      sync.WaitGroup
	id      uuid.UUID
}

func (c *Client) dial(addr string, port string) error {
	var err error
	c.srvAddr, err = net.ResolveUDPAddr("udp", addr+":"+port)
	if err != nil {
		return err
	}

	c.conn, err = net.DialUDP("udp", nil, c.srvAddr)
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) listen() {
	defer c.wg.Done()
	buf := make([]byte, 1024)
	for {
		c.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		select {
		case <-c.stop:
			return
		default:
			n, _, err := c.conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				log.Println("Error reading:", err)
				return
			}
			fmt.Println("Received:", string(buf[:n]))
		}
	}
}

func (c *Client) readStdin() {
	defer c.wg.Done()
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		text := scanner.Text()
		if text == "exit" {
			close(c.stop) // Signal other goroutines to stop
			return
		}
		_, err := c.conn.Write([]byte(text))
		if err != nil {
			log.Println("Error writing:", err)
		}
	}
}

func (c *Client) register(localAddr *net.UDPAddr) error {
	// Create a RegisterRequest message
	req := &api.RegisterRequest{
		LocalEndpoint: &api.Endpoint{IpAddress: localAddr.IP.String(), Port: uint32(localAddr.Port)},
	}

	msg := api.Message{
		Content: &api.Message_RegisterRequest{RegisterRequest: req},
	}
	out, err := proto.Marshal(&msg)
	if err != nil {
		panic(err)
	}

	for i := 0; i < 10; i++ {
		_, err := c.conn.Write(out)
		if err != nil {
			return fmt.Errorf("write error: %w", err)
		}
		log.Println("Register request sent")

		c.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		respBuf := make([]byte, 1024)
		n, _, err := c.conn.ReadFromUDP(respBuf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Println("No registration response received, retrying...")
				continue // Retry
			}
			return fmt.Errorf("read error: %w", err)
		}

		// Process response
		var respMsg api.Message
		err = proto.Unmarshal(respBuf[:n], &respMsg)
		if err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}

		resp, ok := respMsg.Content.(*api.Message_RegisterResponse)
		if !ok {
			return fmt.Errorf("unexpected message type: %T", respMsg.Content)
		}

		registerResponse := resp.RegisterResponse // Access the RegisterResponse message
		if registerResponse.Success {
			c.id = uuid.MustParse(registerResponse.Id)
			log.Println("Registered with server. ID:", c.id)
			c.pubAddr = &net.UDPAddr{
				IP:   net.ParseIP(registerResponse.PublicEndpoint.IpAddress),
				Port: int(registerResponse.PublicEndpoint.Port),
			}
			return nil
		} else {
			return fmt.Errorf("registration failed: %s", registerResponse.Message)
		}
	}
	return fmt.Errorf("failed to register after 10 attempts")
}

func (c *Client) run(addr string, port string) {
	err := c.dial(addr, port)
	if err != nil {
		panic(err)
	}
	localAddr := c.conn.LocalAddr().(*net.UDPAddr)

	c.stop = make(chan struct{})
	c.id = uuid.New()

	if err := c.register(localAddr); err != nil {
		log.Println("Error registering:", err)
		return
	}

	c.wg = sync.WaitGroup{}

	c.wg.Add(1)
	go c.listen()

	c.wg.Add(1)
	go c.readStdin()

	<-c.stop
	log.Println("Closing connection")

	c.wg.Wait()
	c.conn.Close()
	log.Println("Client exited")
}
