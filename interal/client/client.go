package client

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
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

func (c *Client) Run(addr string, port string) {
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
