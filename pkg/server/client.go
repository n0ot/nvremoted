package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/n0ot/nvremoted/pkg/model"
	"github.com/sirupsen/logrus"
)

const sendBuffSize = 10 // Buffer size of channel for sending data to clients

// Client Represents a Client on the server.
type Client struct {
	conn          net.Conn           // IO for the client
	Send          chan model.Message `json:"-"` // Messages sent here will be serialized and written to the client
	ID            uint64             `json:"id"`
	done          chan struct{}      // Closed when client is finished
	StoppedReason string             `json:"stopped_reason"` // Reason the client was stopped
	srv           *Server            // The server this client belongs to
	// Go vet complains about the json tag on remoteHost because it isn't exported, but
	// keeping it so it shows up in the logs.
	remoteHost string `json:"remote_host"`
}

// newClient initializes a new client, and
// adds it to the given server.
func newClient(conn net.Conn, ID uint64, srv *Server, remoteHost string) *Client {
	c := &Client{
		conn:       conn,
		Send:       make(chan model.Message, sendBuffSize),
		ID:         ID,
		done:       make(chan struct{}, 1),
		srv:        srv,
		remoteHost: remoteHost,
	}

	c.srv.addClient(c)

	// Connect Send/Recv channels to the net.Conn.
	finished := make(chan struct{}, 2)
	go c.send(finished)
	go c.receive(finished)
	go c.waitFinished(finished)

	return c
}

// send receives messages on the client's Send channel, serializes it, and sends it to the client.
func (c *Client) send(finished chan<- struct{}) {
	defer func() { finished <- struct{}{} }()
	encoder := json.NewEncoder(c.conn)

	for {
		select {
		case <-c.done:
			return
		case msg, ok := <-c.Send:
			if !ok {
				c.Stop("Server removed client")
				return
			}
			switch msg := msg.(type) {
			case pingMessage:
				if _, err := c.conn.Write([]byte("\n")); err != nil {
					c.srv.log.WithFields(logrus.Fields{
						"server_client": c,
						"error":         err,
					}).Warn("Error while sending ping to client")
					c.Stop("Send error")
					return
				}
				continue
			default:
				if err := encoder.Encode(msg); err != nil {
					c.srv.log.WithFields(logrus.Fields{
						"server_client": c,
						"error":         err,
					}).Warn("Error while marshaling message to client")
					c.Stop("Send error")
					return
				}
			}
		}
	}
}

// receive receives data from the client, marshals it, and sends the resulting message to the server to be handled.
func (c *Client) receive(finished chan<- struct{}) {
	defer func() { finished <- struct{}{} }()
	dec := json.NewDecoder(c.conn)

	for !c.Stopped() {
		if c.srv.readDeadline == 0 {
			c.conn.SetReadDeadline(time.Now().Add(time.Minute))
		} else {
			c.conn.SetReadDeadline(time.Now().Add(c.srv.readDeadline))
		}

		msg, err := unmarshalMessage(dec, c.srv.messages)
		if err == nil {
			msg.Handle(c)
			continue
		}

		if err == io.EOF {
			c.Stop("Client disconnected")
			return
		}
		if terr, ok := err.(net.Error); ok && terr.Timeout() {
			if c.srv.readDeadline == 0 {
				// No timeout enforcement.
				// Decoder breaks if it returns an error; reinitialize.
				dec = json.NewDecoder(c.conn)
				continue
			}
			c.Stop("Client timed out")
			return
		}
		c.srv.log.WithFields(logrus.Fields{
			"server_client": c,
			"error":         err,
		}).Warn("Error unmarshaling message from client")
		c.Stop("Receive error")
		return
	}
}

// waitFinished cleans up after this client has stopped, or when errors were encountered.
func (c *Client) waitFinished(finished <-chan struct{}) {
	<-finished // Wait for send to finish
	<-finished // Wait for receive to finish
	c.srv.removeClient(c)
	// Drain the send channel.
	for range c.Send {
	}
	c.conn.Close()
}

// Stopped returns true if the client was stopped.
func (c *Client) Stopped() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// Stop stops a client, closing it's net.Conn.
// Stop is idempotent; calling Stop more than once will have no effect.
func (c *Client) Stop(reason string) {
	if c.Stopped() {
		return
	}

	c.StoppedReason = reason
	close(c.done)
}

func (c *Client) String() string {
	return fmt.Sprintf("Client %d (%s)", c.ID, c.remoteHost)
}

func unmarshalMessage(dec *json.Decoder, msgs map[string]func() Message) (Message, error) {
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}

	var unknownMSG model.DefaultMessage
	if err := json.Unmarshal(raw, &unknownMSG); err != nil {
		return nil, err
	}

	msgFunc := msgs[unknownMSG.Type]
	var msg Message
	if msgFunc == nil {
		msg = make(DefaultMessage)
	} else {
		msg = msgFunc()
	}

	var err error
	if defaultMSG, ok := msg.(DefaultMessage); ok {
		err = json.Unmarshal(raw, &defaultMSG)
	} else {
		err = json.Unmarshal(raw, &msg)
	}
	if err != nil {
		return nil, err
	}

	return msg, nil
}
