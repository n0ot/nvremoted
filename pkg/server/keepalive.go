package server

import (
	"crypto/tls"
	"net"
	"time"
)

// configureTCPKeepAlive configures the transport even when application pings
// are disabled. The idle period matches Go's default; probe interval and count
// depend on the listener/OS. SetKeepAlivePeriod may reset the probe interval on
// older Windows, so this is not a precise timeout.
func configureTCPKeepAlive(conn net.Conn) error {
	if tlsConn, ok := conn.(*tls.Conn); ok {
		conn = tlsConn.NetConn()
	}
	if socket, ok := conn.(interface {
		SetKeepAlive(bool) error
		SetKeepAlivePeriod(time.Duration) error
	}); ok {
		if err := socket.SetKeepAlive(true); err != nil {
			return err
		}
		return socket.SetKeepAlivePeriod(15 * time.Second)
	}
	return nil
}
