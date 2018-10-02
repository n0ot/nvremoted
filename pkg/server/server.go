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
	TimeBetweenPings         time.Duration
	PingsUntilTimeout        int
	readDeadline             time.Duration
	TLSConfig                *tls.Config
	clientsMTX               sync.RWMutex // Protects clients
	clients                  map[uint64]*Client
	messages                 map[string]func() ServerMessage
	DefaultServerMessageFunc func(*Client, DefaultServerMessage)
	ConnectedFunc            func(*Client)
	DisconnectedFunc         func(*Client)
	log                      *logrus.Logger
}

// New creates a new server.
// After configuring the server, use ListenAndServe or ListenAndServeTLS,
// or call Serve with your own net.Listener to start the server.
func New(log *logrus.Logger) *Server {
	return &Server{
		clients:  make(map[uint64]*Client),
		messages: make(map[string]func() ServerMessage),
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
			c.Send <- pingMessage{"ping"}
		}
		srv.clientsMTX.RUnlock()
	}

	srv.log.Info("Server finished")
}

type pingMessage model.DefaultMessage

func (msg pingMessage) Message() string {
	return "ping"
}

// A ServerMessage is sent to the server from a client.
// When a ServerMessage is received, its Handle(*Client) method is run.
type ServerMessage interface {
	Handle(c *Client)
}

type DefaultServerMessage map[string]interface{}

func (msg DefaultServerMessage) Handle(c *Client) {
	c.srv.DefaultServerMessageFunc(c, msg)
}

// RegisterMessage registers a ServerMessage to the given type.
func (srv *Server) RegisterMessage(name string, f func() ServerMessage) {
	srv.messages[name] = f
}

// addClient adds a client to the server.
func (srv *Server) addClient(c *Client) {
	srv.clientsMTX.Lock()
	srv.clients[c.ID] = c
	srv.clientsMTX.Unlock()
	srv.log.WithFields(logrus.Fields{
		"server_client": c,
	}).Info("Client connected")

	if srv.ConnectedFunc != nil {
		srv.ConnectedFunc(c)
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

	if srv.DisconnectedFunc != nil {
		srv.DisconnectedFunc(c)
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
