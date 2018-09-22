// Package server implements an NVDA Remote server.
package server

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/n0ot/nvremoted/pkg/model"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"crypto/tls"
)

// Server Contains state for an NVRemoted server.
type Server struct {
	TimeBetweenPings    time.Duration
	PingsUntilTimeout   int
	readDeadline        time.Duration
	TLSConfig           *tls.Config
	clientsMTX          sync.RWMutex // Protects clients
	clients             map[uint64]*Client
	handlers            map[string]MessageHandler
	ConnectedHandler    MessageHandler
	DisconnectedHandler MessageHandler
	DefaultHandler      MessageHandler
	log                 *logrus.Logger
}

// New creates a new server.
// After configuring the server, use ListenAndServe or ListenAndServeTLS,
// or call Serve with your own net.Listener to start the server.
func New(log *logrus.Logger) *Server {
	return &Server{
		clients:  make(map[uint64]*Client),
		handlers: make(map[string]MessageHandler),
		log:      log,
	}
}

// ListenAndServe listens for connections on the network, and connects them to the NVDA Remote server.
func (srv *Server) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "Listen")
	}
	defer listener.Close()

	srv.log.WithFields(logrus.Fields{
		"addr":        addr,
		"tls_enabled": false,
	}).Info("Listening for incoming connections")
	srv.Serve(listener)
	return nil
}

// ListenAndServeTLS behaves just like ListenAndServe, but wraps the connection with TLS.
func (srv *Server) ListenAndServeTLS(addr, certFile, keyFile string) error {
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return errors.Wrap(err, "Load X.509 key pair")
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	}
	if srv.TLSConfig == nil {
		return errors.New("No TLSConfig set in server, and no certFile/keyFile given")
	}

	listener, err := tls.Listen("tcp", addr, srv.TLSConfig)
	if err != nil {
		return errors.Wrap(err, "Listen TLS")
	}
	defer listener.Close()

	srv.log.WithFields(logrus.Fields{
		"addr":        addr,
		"tls_enabled": true,
	}).Info("Listening for incoming connections")
	srv.Serve(listener)
	return nil
}

func (srv *Server) acceptClients(listener net.Listener) {
	var nextID uint64
	srv.readDeadline = srv.TimeBetweenPings * time.Duration(srv.PingsUntilTimeout)
	for {
		conn, err := listener.Accept()
		if err != nil {
			srv.log.WithFields(logrus.Fields{
				"error": err,
			}).Error("Error accepting connection")
			continue
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(srv.TimeBetweenPings)
		}

		remoteAddr, _, err := net.SplitHostPort(conn.RemoteAddr().String())
		remoteHost := getHostFromAddrIfPossible(remoteAddr)
		newClient(conn, nextID, srv, remoteHost)
		nextID++
	}
}

// Serve serves clients the NVDA Remote service.
func (srv *Server) Serve(listener net.Listener) {
	srv.log.WithFields(logrus.Fields{
		"time_between_pings":  srv.TimeBetweenPings,
		"pings_until_timeout": srv.PingsUntilTimeout,
	}).Info("Server started")
	timeBetweenPings := srv.TimeBetweenPings
	// If timeBetweenPings is 0,
	// the pings chan will remain nil, and the ping handling will never be called.
	var pings <-chan time.Time
	if timeBetweenPings > 0 {
		ticker := time.NewTicker(timeBetweenPings)
		defer ticker.Stop()
		pings = ticker.C
	}
	go srv.acceptClients(listener)

	for _ = range pings {
		srv.clientsMTX.RLock()
		for _, c := range srv.clients {
			if c == nil {
				continue
			}
			c.Send <- nil // Translated to client as newline
		}
		srv.clientsMTX.RUnlock()
	}

	srv.log.Info("Server finished")
}

// A MessageHandler defines behavior for handling incoming messages.
type MessageHandler interface {
	Handle(c *Client, msg model.Message)
}

// MessageHandlerFunc is an adapter to turn an ordinary function into a MessageHandler.
type MessageHandlerFunc func(c *Client, msg model.Message)

func (f MessageHandlerFunc) Handle(c *Client, msg model.Message) {
	f(c, msg)
}

// HandleMessage registers a MessageHandler to the given message type.
func (srv *Server) HandleMessage(name string, h MessageHandler) {
	srv.handlers[name] = h
}

// handle handles an incoming message
func (srv *Server) handle(c *Client, msg model.Message) {
	t, ok := msg["type"].(string)
	if !ok {
		c.Send <- model.ErrorMessage("No type in message")
		return
	}
	h, ok := srv.handlers[t]
	if !ok {
		h = srv.DefaultHandler
	}
	if h == nil {
		c.Send <- model.ErrorMessage("Unrecognized command")
		return
	}

	h.Handle(c, msg)
}

// addClient adds a client to the server.
func (srv *Server) addClient(c *Client) {
	srv.clientsMTX.Lock()
	srv.clients[c.ID] = c
	srv.clientsMTX.Unlock()
	srv.log.WithFields(logrus.Fields{
		"server_client": c,
	}).Info("Client connected")

	if srv.ConnectedHandler != nil {
		srv.ConnectedHandler.Handle(c, model.Message{
			"type": "connected",
		})
	}
}

// removeClient removes a client from the server.
func (srv *Server) removeClient(c *Client) {
	srv.clientsMTX.Lock()
	delete(srv.clients, c.ID)
	srv.clientsMTX.Unlock()
	srv.log.WithFields(logrus.Fields{
		"server_client": c,
		"reason":        c.StoppedReason,
	}).Info("Client disconnected")

	if srv.DisconnectedHandler != nil {
		srv.DisconnectedHandler.Handle(c, model.Message{
			"type": "disconnected",
		})
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
