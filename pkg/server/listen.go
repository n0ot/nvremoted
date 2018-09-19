package server

import (
	"crypto/tls"
	"log"
	"net"

	"github.com/pkg/errors"
)

// ListenAndServe listens for connections on the network, and connects them to the NVDA Remote server.
func (server *Server) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "Listen")
	}
	defer listener.Close()

	log.Printf("Listening on %s", addr)
	go server.acceptClients(listener)
	server.Serve(listener)
	return nil
}

// ListenAndServeTLS behaves just like ListenAndServe, but wraps the connection with TLS.
func (server *Server) ListenAndServeTLS(addr, certFile, keyFile string) error {
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return errors.Wrap(err, "Load X.509 key pair")
		}
		server.config.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	}
	if server.config.TLSConfig == nil {
		return errors.New("No TLSConfig set in server, and no certFile/keyFile given")
	}

	listener, err := tls.Listen("tcp", addr, server.config.TLSConfig)
	if err != nil {
		return errors.Wrap(err, "Listen TLS")
	}
	defer listener.Close()

	log.Printf("Listening on %s with TLS enabled", addr)
	go server.acceptClients(listener)
	server.Serve(listener)
	return nil
}

func (srv *Server) acceptClients(listener net.Listener) {
	var nextID uint64
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %s", err)
			continue
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(srv.config.TimeBetweenPings)
		}

		client := NewClient(conn, nextID, relayClientHandler{srv})
		nextID++

		remoteAddr, _, err := net.SplitHostPort(conn.RemoteAddr().String())
		remoteHost := getHostFromAddrIfPossible(remoteAddr)
		log.Printf("Connected: %s from %s", client, remoteHost)
	}
}
