package main

import (
	"fmt"
	"log"
	"net"
	"time"

	api "natpunch/proto/gen/go"

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

		select {
		case <-c.registered:
			return nil // Registration successful
		case <-time.After(RegReadTimeout):
			log.Println("No registration response received, retrying...")
			continue // Retry
		case <-c.stop:
			return fmt.Errorf("registration cancelled")
		}
	}
	return fmt.Errorf("failed to register after %d attempts", MaxRegistrationAttempts)
}
