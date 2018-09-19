package server

import (
	"github.com/n0ot/nvremoted/pkg/models"
	log "github.com/sirupsen/logrus"
)

// ClientHandler handles messages to and from clients.
// ClientHandlers can call other ClientHandlers,
// but will be blocked until the called handler returns.
// Receive from client.Recv, and respond to client.Send.
type ClientHandler interface {
	Handle(*Client) string
}

// defaultClientHandler is not a ClientHandler itself, but concrete ClientHandlers
// alias this and override Handle().
type defaultClientHandler struct {
	srv *Server
}

// relayClientHandler connects a client to the NVDA Remote service.
type relayClientHandler defaultClientHandler

func (ch relayClientHandler) Handle(client *Client) string {
	// Responses will be read from the NVDA Remote service via resp, and sent to the client.
	resp := make(chan models.Message)

	// If the client disconnects before the service is done sending to resp,
	// the channel could fill up, which would hang the service.
	// Make sure it is empty
	defer func() {
		for _ = range resp {
		}
	}()

	ch.srv.in <- &Command{
		Name:   "addclient",
		Client: client,
		Resp:   resp,
	}
	for {
		select {
		case resp, ok := <-resp:
			if !ok {
				// Server closes resp to kic a client
				client.Send <- models.Message(map[string]interface{}{
					"type": "kick",
				})
				return "Disconnected by server"
			}
			client.Send <- resp

		case msg, ok := <-client.Recv:
			if !ok {
				ch.srv.in <- &Command{
					Client: client,
					Resp:   resp,
					Name:   "rmclient",
				}
				return "Client disconnected"
			}

			if msg != nil {
				if cmd, err := parseCommand(msg, client, resp); err != nil {
					log.Printf("Cannot parse command from %s: %s", client, err)
				} else {
					ch.srv.in <- cmd
				}
			}
		}
	}
}
