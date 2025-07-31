package main

import (
	"fmt"
	"log"
	"net"
	"time"

	api "natpunch/proto"

	"google.golang.org/protobuf/proto"
)

// MaxRegistrationAttempts defines the maximum number of registration attempts.
const MaxRegistrationAttempts = 10

// ReadTimeout defines the timeout for reading the registration response.
const RegReadTimeout = 1 * time.Second

// createRegisterMessage creates a new RegisterRequest message.
func (c *Client) createRegisterMessage(localAddr *net.UDPAddr) ([]byte, error) {
	req := &api.RegisterRequest{
		LocalEndpoint: &api.Endpoint{IpAddress: localAddr.IP.String(), Port: uint32(localAddr.Port)},
	}

	msg := api.Message{
		Content: &api.Message_RegisterRequest{RegisterRequest: req},
	}

	out, err := proto.Marshal(&msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal register request: %w", err)
	}
	return out, nil
}

// register registers the client with the server.
// It is blocking and will not return until the client has been registered or
// it gave up trying after MaxRegistrationAttempts, returning an error.
func (c *Client) register(localAddr *net.UDPAddr) error {
	registerMessage, err := c.createRegisterMessage(localAddr)
	if err != nil {
		return err // Return error to the function caller.
	}

	for i := 0; i < MaxRegistrationAttempts; i++ {
		// Send registration request
		_, err = c.conn.WriteTo(registerMessage, c.srvAddr)
		if err != nil {
			return fmt.Errorf("write error: %w", err)
		}
		log.Println("Register request sent")

		// Set read deadline for the response
		c.conn.SetReadDeadline(time.Now().Add(RegReadTimeout))

		// Read response
		respBuf := make([]byte, 1024)
		n, _, err := c.conn.ReadFromUDP(respBuf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Println("No registration response received, retrying...")
				continue // Retry
			}
			return fmt.Errorf("read error: %w", err)
		}

		// Unmarshal response message
		var respMsg api.Message
		err = proto.Unmarshal(respBuf[:n], &respMsg)
		if err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}

		// Check response type
		resp, ok := respMsg.Content.(*api.Message_RegisterResponse)
		if !ok {
			return fmt.Errorf("unexpected message type: %T", respMsg.Content)
		}

		// Process registration response
		registerResponse := resp.RegisterResponse
		if registerResponse.Success {
			c.id = registerResponse.ClientId
			log.Println("Registered with server. ID:", c.id)

			c.pubAddr = &net.UDPAddr{
				IP:   net.ParseIP(registerResponse.PublicEndpoint.IpAddress),
				Port: int(registerResponse.PublicEndpoint.Port),
			}
			return nil // Registration successful
		}
		return fmt.Errorf("registration failed: %s", registerResponse.Message)

	}
	return fmt.Errorf("failed to register after %d attempts", MaxRegistrationAttempts)
}
