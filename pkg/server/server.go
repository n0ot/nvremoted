// Copyright © 2019 Niko Carpenter <nikoacarpenter@gmail.com>
//
// This source code is governed by the MIT license, which can be found in the LICENSE file.

// Package server implements an NVDA Remote server.
package server

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"crypto/tls"
)

// Server Contains state for an NVRemoted server.
type Server struct {
	// TimeBetweenPings specifies the amount of time that will elapse before clients will be sent a ping.
	// If 0, no pings will be sent.
	TimeBetweenPings time.Duration

	// PingsUntilTimeout specifies the number of pings to be sent before unresponsive clients will be kicked.
	// If TimeBetweenPings is 0, this field has no effect.
	PingsUntilTimeout int

	// TLSConfig optionally provides a TLS configuration for use by ListenAndServeTLS.
	TLSConfig *tls.Config

	// MOTD contains the message of the day, which will be sent to clients when connecting.
	MOTD string

	// StatsPassword sets the password for retreiving stats.
	StatsPassword string

	Log *logrus.Logger

	// registry stores information about clients and channels on the server.
	registry registry
}

// ListenAndServe listens for connections on the network, and connects them to the NVDA Remote server.
func (srv *Server) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "Listen")
	}
	defer listener.Close()

	srv.Log.WithFields(logrus.Fields{
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

	srv.Log.WithFields(logrus.Fields{
		"addr":        addr,
		"tls_enabled": true,
	}).Info("Listening for incoming connections")
	srv.Serve(listener)
	return nil
}

func (srv *Server) acceptClients(listener net.Listener) {
	var nextID uint64
	for {
		conn, err := listener.Accept()
		if err != nil {
			srv.Log.WithFields(logrus.Fields{
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
		srv.serveClient(conn, nextID, remoteHost)
		nextID++
	}
}

// Serve serves clients the NVDA Remote service.
func (srv *Server) Serve(listener net.Listener) {
	srv.Log.WithFields(logrus.Fields{
		"time_between_pings":  srv.TimeBetweenPings,
		"pings_until_timeout": srv.PingsUntilTimeout,
	}).Info("Server started")

	now := time.Now()
	srv.registry = registry{
		clients:         make(map[uint64]channelMember),
		channels:        make(map[string]*channel),
		statsPassword:   srv.StatsPassword,
		createdTime:     now,
		maxChannelsTime: now,
		maxClientsTime:  now,
	}
	go srv.acceptClients(listener)

	// Setup a ping timer to periodically ping clients.
	// If timeBetweenPings is 0,
	// pingsCH will remain nil, and clients will not be pinged.
	var pingsCH <-chan time.Time
	if srv.TimeBetweenPings > 0 {
		ticker := time.NewTicker(srv.TimeBetweenPings)
		defer ticker.Stop()
		pingsCH = ticker.C
	}
	pingMSG := pingMessage{}

	for {
		select {
		case <-pingsCH:
			srv.registry.lock.RLock()
			for _, member := range srv.registry.clients {
				member.events <- pingMSG
			}
			srv.registry.lock.RUnlock()
		}
	}
}

type pingMessage struct{}

func (pingMessage) Name() string {
	return "ping"
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
