// Package server implements an NVDA Remote server.
package server

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/n0ot/nvremoted/pkg/channels"
	"github.com/n0ot/nvremoted/pkg/models"
	log "github.com/sirupsen/logrus"

	"crypto/tls"
)

const acceptBuffSize = 10 // Buffer size for channel that accepts server commands

// Server Contains state for an NVRemoted server.
type Server struct {
	config              Config
	channels            map[string]*channels.Channel
	clients             map[uint64]*Client
	clientActiveChannel map[uint64]*channels.Channel
	clientResp          map[uint64]chan<- models.Message
	in                  chan *Command // Server accepts commands on this channel
}

// Config  holds the externally configurable settings for a server.
type Config struct {
	ServerName         string
	TimeBetweenPings   time.Duration
	PingsUntilTimeout  int
	TLSConfig          *tls.Config
	Motd               string
	WarnIfNotEncrypted bool
}

// NewServer creates a new server with the specified configuration
func NewServer(config *Config) *Server {
	server := Server{
		config:              *config,
		channels:            make(map[string]*channels.Channel),
		clients:             make(map[uint64]*Client),
		clientActiveChannel: make(map[uint64]*channels.Channel),
		clientResp:          make(map[uint64]chan<- models.Message),
		in:                  make(chan *Command, acceptBuffSize),
	}

	return &server
}

// Serve serves clients the NVDA Remote service.
func (server *Server) Serve(listener net.Listener) {
	log.Printf("Starting server with configuration:\n%+v", server.config)

	timeBetweenPings := server.config.TimeBetweenPings
	pingsUntilTimeout := server.config.PingsUntilTimeout

	// If timeBetweenPings is 0,
	// the pings chan will remain nil, and the ping handling will never be called.
	var pings <-chan time.Time
	if timeBetweenPings > 0 {
		ticker := time.NewTicker(timeBetweenPings)
		defer ticker.Stop()
		pings = ticker.C
	}

	for {
		select {
		case command, ok := <-server.in:
			if !ok {
				break
			}
			if err := server.handleCommand(command); err != nil {
				log.Printf("Error while processing command: %s", err)
				if command.Resp != nil {
					command.Resp <- models.ErrorMessage("Internal error")
				}
			}

		case <-pings:
			for id, client := range server.clients {
				if client == nil {
					continue
				}
				if pingsUntilTimeout > 0 && time.Since(client.LastSeen) > timeBetweenPings*time.Duration(pingsUntilTimeout) {
					server.Kick(client.ID, "ping timeout")
					continue
				}
				if resp := server.clientResp[id]; resp != nil {
					resp <- models.Message(map[string]interface{}{
						"type": "ping",
					})
				}
			}
		}
	}
}

// getHostFromAddrIfPossible tries to get the reverse dns host for an address.
// If that isn't possible, it just returns the address.
func getHostFromAddrIfPossible(addr string) string {
	var hosts string
	names, err := net.LookupAddr(addr)
	if err == nil { // No need to report errors; just fallback to IP
		hosts = strings.Join(names, ", ")
	}

	if hosts == "" {
		return addr
	}

	return fmt.Sprintf("%s (%s)", hosts, addr)
}
