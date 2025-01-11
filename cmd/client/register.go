package main

import (
	"fmt"
	"log"
	"net"
	"time"

	api "natpunch/proto/gen/go"

	"google.golang.org/protobuf/proto"
)

// Register registers the client with the server. It is blocking
// and will not return until the client has been registered or
// it gave up trying to do so, returning an error.
// Cannot be called concurrently, no go routines allowed here.
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
		_, err = c.conn.WriteTo(out, c.srvAddr)
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
			c.id = registerResponse.ClientId
			log.Println("Registered with server. ID:", c.id)
			c.pubAddr = &net.UDPAddr{
				IP:   net.ParseIP(registerResponse.PublicEndpoint.IpAddress),
				Port: int(registerResponse.PublicEndpoint.Port),
			}
			return nil
		}
		return fmt.Errorf("registration failed: %s", registerResponse.Message)
	}
	return fmt.Errorf("failed to register after 10 attempts")
}
