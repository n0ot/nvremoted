// Copyright © 2023 Niko Carpenter <niko@nikocarpenter.com>
//
// This source code is governed by the MIT license, which can be found in the LICENSE file.

package server

import (
	"encoding/json"
	"io"
	"net"
	"sync"

	"github.com/sirupsen/logrus"
)

// client represents a client on the server.
type client struct {
	id         uint64
	conn       net.Conn
	events     chan Message  // passes internal messages to a client
	recv       chan Message  // passes messages to a client from the network
	readNext   chan struct{} // Used by handleClient to ask readFromClient to read the next message
	channel    *channel      // active channel
	registry   *registry
	encoder    *json.Encoder
	stopMTX    sync.RWMutex // Protects stopped and stopReason
	stopped    bool
	stopReason string
	log        *logrus.Logger
}

// serveClient handles events sent and received by a client.
func (srv *Server) serveClient(conn net.Conn, id uint64, remoteHost string) {
	c := &client{
		id:       id,
		conn:     conn,
		events:   make(chan Message, 1),
		recv:     make(chan Message),
		readNext: make(chan struct{}),
		registry: &srv.registry,
		encoder:  json.NewEncoder(conn),
		log:      srv.Log,
	}

	// Wait for both goroutines before removing channel and registry state.
	finished := make(chan struct{}, 2)

	srv.Log.WithFields(logrus.Fields{
		"id":          id,
		"remote_host": remoteHost,
	}).Info("Client connected")

	go srv.readFromClient(c, finished)
	go srv.handleClient(c, finished)
	go func() {
		// Wait for both readFromClient and handleClient to finish
		<-finished
		<-finished

		// The active channel and server registry may still be sending events to the client after requesting removal.
		// The events channel needs to be closed and drained to prevent these goroutines from hanging.
		if c.channel != nil {
			c.channel.leave(c.id)
		}

		close(c.events)
		for range c.events {
		}

		conn.Close()
		srv.Log.WithFields(logrus.Fields{
			"id":          id,
			"remote_host": remoteHost,
			"reason":      c.stopReason,
		}).Info("Client disconnected")
	}()
}

// readFromClient reads data from the client socket, marshals it, and sends the resulting clientMessage to the client's events channel to be handled.
func (srv *Server) readFromClient(c *client, finished chan<- struct{}) {
	defer func() {
		close(c.recv)
		finished <- struct{}{}
	}()

	// NVDA Remote clients do not acknowledge server pings. Silence is valid;
	// transport errors detect dead peers, and stop closes the socket to unblock us.
	dec := json.NewDecoder(c.conn)

	for !c.isStopped() {
		msg, err := unmarshalClientMessage(c.id, dec)
		// handleClient could have finished while the above read was blocking.
		if err == nil {
			c.recv <- msg
			// Sending the unmarshaled message to handleClient might cause the client to be kicked.
			// But there would be no wayfor this goroutine to know that until the next read operation unblocks.
			// By waiting for handleClient to signal that it has finished processing the message,
			// we are able to see if the client was stopped before trying to read from the socket again.
			<-c.readNext
			continue
		}

		if c.isStopped() {
			return
		}
		if err == io.EOF {
			c.stop("Client disconnected")
			return
		}
		if _, ok := err.(*json.UnmarshalTypeError); ok {
			c.sendError("malformed message")
			c.stop("client sent a malformed request")
			return
		}
		srv.Log.WithFields(logrus.Fields{
			"id":    c.id,
			"error": err,
		}).Warn("Error unmarshaling message from client")
		c.stop("Receive error")
		return
	}
}

// handleClient handles events sent on the client's events channel, serializes outgoing messages, and sends them to the client.
func (srv *Server) handleClient(c *client, finished chan<- struct{}) {
	defer func() {
		finished <- struct{}{}
	}()

	// Send the MOTD when the client connects
	if srv.MOTD != "" {
		c.send(ClientMOTDResponse{
			Type: "motd",
			MOTD: srv.MOTD,
		})
	}

	for {
		select {
		case msg, ok := <-c.recv:
			if !ok {
				return // The client was stopped.
			}

			if handlerFunc := clientMessageHandlers[msg.Name()]; handlerFunc == nil {
				c.log.WithFields(logrus.Fields{
					"id":           c.id,
					"message_name": msg.Name(),
				}).Warn("No handler found for client message")
				c.sendInternalError()
				c.stop("internal error")
			} else {
				handlerFunc(c, msg)
			}
			// Tell readFromClient to read the next message
			c.readNext <- struct{}{}

		case msg := <-c.events:
			if handlerFunc := clientEventHandlers[msg.Name()]; handlerFunc == nil {
				c.log.WithFields(logrus.Fields{
					"id":           c.id,
					"message_name": msg.Name(),
				}).Warn("No handler found for client event")
				c.sendInternalError()
				c.stop("internal error")
			} else {
				handlerFunc(c, msg)
			}
		}
	}
}

// stop stops a client with the specified reason
// This method is safe to use concurrently.
func (c *client) stop(reason string) {
	c.stopMTX.Lock()
	if c.stopped {
		c.stopMTX.Unlock()
		return
	}
	c.stopped = true
	c.stopReason = reason
	c.stopMTX.Unlock()

	// Wake blocked reads and writes so cleanup does not need an idle timeout.
	c.conn.Close()
}

// isStopped checks to see if a client is stopped.
// This method is safe to use concurrently.
func (c *client) isStopped() bool {
	var stopped bool
	c.stopMTX.RLock()
	stopped = c.stopped
	c.stopMTX.RUnlock()
	return stopped
}

func (c *client) send(resp Message) {
	if err := c.encoder.Encode(resp); err != nil {
		c.log.WithFields(logrus.Fields{
			"id":    c.id,
			"error": err,
		}).Warn("Error while marshaling response to client")
		c.stop("Send error")
	}
}

func (c *client) sendError(reason string) {
	c.send(ClientErrorResponse{
		Type:  "error",
		Error: reason,
	})
}

func (c *client) sendInternalError() {
	c.sendError("internal error")
}

func unmarshalClientMessage(id uint64, dec *json.Decoder) (Message, error) {
	// The raw JSON needs to be stored, because it will be unmarshalled twice,
	// first to a GenericClientMessage to get its type, then to the more specific Message type.
	// All returned messages will implement clientMessage, except for those of type message.ChannelMessage.
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}

	var genericMSG GenericClientMessage
	if err := json.Unmarshal(raw, &genericMSG); err != nil {
		return nil, err
	}

	// If genericMSG.Type corresponds to a known clientMessage,
	// msgFunc will return a new empty message of that type into which the JSON will be unmarshalled.
	msgFunc := clientMessages[genericMSG.Type]
	var msg Message
	var err error
	if msgFunc == nil {
		// There is no clientMessage with the specified type.
		// Because the NVDA Remote protocol allows arbitrary messages to be sent on channels,
		// the JSON needs to be marshalled into a map.
		m := make(map[string]interface{})
		err = json.Unmarshal(raw, &m)
		msg = &channelMessage{
			origin: id,
			msg:    m,
		}
	} else {
		msg = msgFunc()
		err = json.Unmarshal(raw, &msg)
	}

	if err != nil {
		return nil, err
	}

	return msg, nil
}
