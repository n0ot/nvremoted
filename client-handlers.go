package nvremoted

import log "github.com/sirupsen/logrus"

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
	server *Server
}

// initServerClientHandler should be passed to NewClient as the initial client handler.
// It sends the MOTD, and requires they join a channel.
type initServerClientHandler defaultClientHandler

func (ch initServerClientHandler) Handle(client *Client) string {
	if ch.server.config.Motd != "" {
		client.Send <- message(map[string]interface{}{
			"type": "motd",
			"motd": ch.server.config.Motd,
		})
	}
	return relayClientHandler{ch.server}.Handle(client)
}

// relayClientHandler connects a client to the NVDA Remote service.
type relayClientHandler defaultClientHandler

func (ch relayClientHandler) Handle(client *Client) string {
	// Responses will be read from the NVDA Remote service via responseChan, and sent to the client.
	responseChan := make(chan message)

	// If the client disconnects before the service is done sending to responseChan,
	// the channel could fill up, which would hang the service.
	// Make sure it is empty
	defer func() {
		for _ = range responseChan {
		}
	}()

	ch.server.in <- &serverCommand{
		command:      "addclient",
		client:       client,
		responseChan: responseChan,
	}
	for {
		select {
		case resp, ok := <-responseChan:
			if !ok {
				// Server closes responseChan to kic a client
				client.Send <- kickMessage
				return "Disconnected by server"
			}
			client.Send <- resp

		case msg, ok := <-client.Recv:
			if !ok {
				ch.server.in <- &serverCommand{
					client:       client,
					responseChan: responseChan,
					command:      "rmclient",
				}
				return "Client disconnected"
			}

			if msg != nil {
				if cmd, err := parseCommand(msg, client, responseChan); err != nil {
					log.Printf("Cannot parse command from %s: %s", client, err)
				} else {
					ch.server.in <- cmd
				}
			}
		}
	}
}
