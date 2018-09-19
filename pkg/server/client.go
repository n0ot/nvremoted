package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/n0ot/nvremoted/pkg/models"
	log "github.com/sirupsen/logrus"
)

const sendBuffSize = 10 // Buffer size of channel for sending data to clients

// Client Represents a client on the server.
// It is the ClientHandler's responsibility to make sure nothing sends on Client.Send
// after the client is stopped, as Client.Send will be closed.
type Client struct {
	conn          net.Conn            // IO for the client
	Send          chan models.Message // Messages sent here will be serialized and written to the client
	Recv          chan models.Message // Messages received by client will be deserialized and sent here.
	ID            uint64
	done          chan struct{} // Closed when client is finished
	StoppedReason string        // Reason the client was stopped
	LastSeen      time.Time
}

// NewClient initializes a new client, and
// calls the given client handler.
// When the ClientHandler returns,
// the client will be disconnected.
func NewClient(conn net.Conn, ID uint64, clientHandler ClientHandler) *Client {
	client := &Client{
		conn:     conn,
		Send:     make(chan models.Message, sendBuffSize),
		Recv:     make(chan models.Message),
		ID:       ID,
		done:     make(chan struct{}, 1),
		LastSeen: time.Now(),
	}

	// Connect Send/Recv channels to the net.Conn.
	finished := make(chan struct{}, 1)
	go client.send(finished)
	go client.receive()
	go client.handle(clientHandler, finished)

	return client
}

// send receives messages on the client's Send channel, serializes it, and sends it to the client.
func (client *Client) send(finished chan<- struct{}) {
	defer func() { finished <- struct{}{} }()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Error sending data from client handler to %s: %s", client, r)
		}
		log.Printf("Stopping pipe from client handler to %s", client)
	}()
	log.Printf("Starting pipe from client handler to %s", client)
	encoder := json.NewEncoder(client.conn)

	for msg := range client.Send {
		if err := encoder.Encode(msg); err != nil {
			log.Printf("Error while serializing message to %s: %s", client, err)
			client.Stop("Send error")
			break
		}
	}
}

// receive receives data from the client, serializes it, and sends the resulting message to client.Recv.
func (client *Client) receive() {
	defer close(client.Recv)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Error sending data from %s to client handler: %s", client, r)
		}
		log.Printf("Stopping pipe from %s to client handler", client)
	}()
	log.Printf("Starting pipe from %s to client handler", client)
	decoder := json.NewDecoder(client.conn)

	var msg models.Message
	for !client.Stopped() {
		client.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		msg = nil // Otherwise, new json would be merged into the existing map.
		if err := decoder.Decode(&msg); err != nil {
			// If recovering from an error, readers of client.Recv
			// should ignore nil values.
			msg = nil
			if err == io.EOF {
				client.Stop("Client disconnected")
				return
			}
			if err, ok := err.(net.Error); ok && err.Timeout() {
				// We're only using timeouts to give a client receivin no data a chance to clean up
				// if it was stopped.
				//
				// Once the JSON decoder receives an error, it's useless, and we need to make another.
				decoder = json.NewDecoder(client.conn)
			} else {
				log.Printf("Error deserializing message from client: %s", err)
				client.Stop("Receive error")
				return
			}
		}

		select {
		case client.Recv <- msg:
			if msg != nil {
				client.LastSeen = time.Now()
			}
		case <-client.done:
			break
		}
	}
}

// handle Passes messages between a client and a client handler.
// Waits for the client handler to finish,
// and then stops the client.
func (client *Client) handle(clientHandler ClientHandler, finished <-chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Error handling %s: %s", client, r)
		}
	}()

	log.Printf("Handling %s", client)
	exitReason := clientHandler.Handle(client)
	client.Stop(exitReason)
	reasonStrs := make([]string, 0, 2)
	reasonStrs = append(reasonStrs, fmt.Sprintf("%s exited; reason: %s", client, client.StoppedReason))
	if client.StoppedReason != exitReason {
		reasonStrs = append(reasonStrs, fmt.Sprintf("client handler returned: %s", exitReason))
	}
	log.Println(strings.Join(reasonStrs, "; "))

	// As client.Send should only be used by client handlers,
	// it is safe to close now.
	close(client.Send)

	<-finished // Wait for send to finish

	// Wait for receive to finish.
	for _ = range client.Recv {
	}
	client.conn.Close()
}

// Stopped returns true if the client was stopped.
func (client *Client) Stopped() bool {
	select {
	case <-client.done:
		return true
	default:
		return false
	}
}

// Stop stops a client, closing it's ReadWriteCloser.
// Stop is idempotent; calling Stop more than once will have no effect.
// The send channel will not be closed until the initial ClientHandler returns.
func (client *Client) Stop(reason string) {
	if client.Stopped() {
		return
	}

	client.StoppedReason = reason
	close(client.done)
}

func (client *Client) String() string {
	return fmt.Sprintf("Client(%d)", client.ID)
}
