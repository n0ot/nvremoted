// Package nvremoted implements an NVDA Remote server.
package nvremoted

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/pkg/errors"

	"crypto/tls"
)

const acceptBuffSize = 10 // Buffer size of channel for accepting commands

// Server Contains state for an NVRemoted Server
type Server struct {
	config              ServerConfig
	channels            map[string]*channel
	clients             map[uint64]*Client
	clientActiveChannel map[uint64]string
	clientResponseChan  map[uint64]chan<- message
	in                  chan *serverCommand // Server accepts commands on this channel
	runningLock         sync.Mutex          // protects running
	running             bool
}

// ServerConfig  holds the externally configurable settings for a server.
type ServerConfig struct {
	ServerName         string
	BindAddr           string
	TimeBetweenPings   time.Duration
	PingsUntilTimeout  int
	CertFile           string
	KeyFile            string
	UseTLS             bool
	Motd               string
	WarnIfNotEncrypted bool
}

// NewServer creates a new server with the specified configuration
func NewServer(config *ServerConfig) *Server {
	server := Server{
		config:              *config,
		channels:            make(map[string]*channel),
		clients:             make(map[uint64]*Client),
		clientActiveChannel: make(map[uint64]string),
		clientResponseChan:  make(map[uint64]chan<- message),
		in:                  make(chan *serverCommand, acceptBuffSize),
	}

	return &server
}

// Start starts the NVDA Remote server on the given host/port
func (server *Server) Start() {
	server.runningLock.Lock()
	if server.running {
		server.runningLock.Unlock()
		log.Printf("Server is already running")
		return
	}
	server.running = true
	server.runningLock.Unlock()

	log.Printf("Starting server with configuration:\n%+v", server.config)
	var listener net.Listener
	var listenerErr error
	if server.config.UseTLS {
		cert, err := tls.LoadX509KeyPair(server.config.CertFile, server.config.KeyFile)
		if err != nil {
			log.Printf("Error loading X.509 key pair: %s", err)
			return
		}

		tlsConf := &tls.Config{Certificates: []tls.Certificate{cert}}
		listener, listenerErr = tls.Listen("tcp", server.config.BindAddr, tlsConf)
		if listenerErr != nil {
			log.Printf("Cannot start the server, binding on %s; %s", server.config.BindAddr, listenerErr)
			return
		}
		log.Printf("Listening on %s with TLS enabled", server.config.BindAddr)
	} else {
		listener, listenerErr = net.Listen("tcp", server.config.BindAddr)
		if listenerErr != nil {
			log.Printf("Cannot start the server, binding on %s; %s", server.config.BindAddr, listenerErr)
			return
		}
		log.Printf("Listening on %s", server.config.BindAddr)
	}

	defer listener.Close()
	go server.acceptCommands()

	var nextID uint64
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %s", err)
			continue
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(15 * time.Second)
		}

		client, err := NewClient(conn, nextID, initServerClientHandler{server})
		if err != nil {
			log.Printf("Error creating client: %s", err)
			continue
		}
		nextID++

		remoteAddr, _, err := net.SplitHostPort(conn.RemoteAddr().String())
		remoteHost := getHostFromAddrIfPossible(remoteAddr)
		log.Printf("Connected: %s from %s", client, remoteHost)
	}
}

// Receives commands from the server's incoming channel, and processes them.
func (server *Server) acceptCommands() {
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
				if command.responseChan != nil {
					command.responseChan <- errorMessage("Internal error")
				}
			}

		case <-pings:
			for id, client := range server.clients {
				if client == nil {
					continue
				}
				if pingsUntilTimeout > 0 && time.Since(client.LastSeen) > timeBetweenPings*time.Duration(pingsUntilTimeout) {
					server.kick(client.ID, "ping timeout")
					continue
				}
				if responseChan := server.clientResponseChan[id]; responseChan != nil {
					responseChan <- message(map[string]interface{}{
						"type": "ping",
					})
				}
			}
		}
	}
}

// handleCommand looks up a command in the internalCommands or commands map, found in server-commands.go,
// and if found, runs it.
func (server *Server) handleCommand(command *serverCommand) error {
	defer func() {
		if r := recover(); r != nil {
			errors.Errorf("Command: %s, %s: %s", command.command, command.client, r)
		}
	}()

	if command.client == nil {
		return errors.Errorf("No client supplied in command")
	}

	responseChan := command.responseChan
	if responseChan == nil {
		return errors.Errorf("Received command, but no response channel; command: %q", command)
	}

	if command.command == "" {
		return errors.New("Command name is blank")
	}

	if _, ok := server.clients[command.client.ID]; !ok && command.clientInitiated {
		// If a client sends commands quickly, but is disconnected before all of them run,
		// the commands received here after the client was removed should be ignored.
		// This is not an error.
		return nil
	}

	var handler commandHandler
	// If the command was run internally, it also has access to the internalCommands mapping
	if !command.clientInitiated {
		handler = internalCommands[command.command]
	}

	// If a handler was found at this point,
	// don't override it with a client command.
	if handler == nil {
		handler = commands[command.command]
	}

	if handler == nil {
		// Relay all unknown commands to the channel for other clients to pick up.
		if server.clientActiveChannel[command.client.ID] == "" {
			responseChan <- errorMessage("Type unknown, and not in a channel to relay")
			return nil
		}

		msg := message(make(map[string]interface{}))
		for k, v := range command.args {
			msg[k] = v
		}
		msg["origin"] = command.client.ID
		excludeIDs := map[uint64]struct{}{command.client.ID: struct{}{}}
		return relayToChannel(server, server.clientActiveChannel[command.client.ID], msg, excludeIDs)
	}

	handler.Handle(server, command)
	return nil
}

// kick kicks a client from the server.
func (server *Server) kick(clientID uint64, reason string) error {
	if _, ok := server.clients[clientID]; !ok {
		errors.Errorf("Client(%s) not found; cannot kick", clientID)
	}

	if channelName := server.clientActiveChannel[clientID]; channelName != "" {
		// Remove the client from the channel they're in
		leaveChannel(server, clientID, channelName, reason)
	}

	close(server.clientResponseChan[clientID]) // Signals client handler to kick client.
	delete(server.clientResponseChan, clientID)
	delete(server.clients, clientID)

	return nil
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
