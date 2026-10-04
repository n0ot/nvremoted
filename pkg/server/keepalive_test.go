package server

import (
	"crypto/tls"
	"errors"
	"net"
	"testing"
	"time"
)

type keepAliveTestConn struct {
	net.Conn
	enabled   bool
	period    time.Duration
	calls     int
	err       error
	periodErr error
}

func (c *keepAliveTestConn) SetKeepAlive(enabled bool) error {
	c.calls++
	c.enabled = enabled
	return c.err
}

func (c *keepAliveTestConn) SetKeepAlivePeriod(period time.Duration) error {
	c.calls++
	c.period = period
	return c.periodErr
}

func TestConfigureTCPKeepAlive(t *testing.T) {
	for _, useTLS := range []bool{false, true} {
		t.Run(map[bool]string{false: "TCP", true: "TLS"}[useTLS], func(t *testing.T) {
			socket := &keepAliveTestConn{}
			var conn net.Conn = socket
			if useTLS {
				conn = tls.Server(conn, &tls.Config{})
			}
			if err := configureTCPKeepAlive(conn); err != nil {
				t.Fatal(err)
			}
			if !socket.enabled || socket.period != 15*time.Second || socket.calls != 2 {
				t.Errorf("enabled=%v period=%v calls=%d", socket.enabled, socket.period, socket.calls)
			}
		})
	}
}

func TestConfigureTCPKeepAliveErrors(t *testing.T) {
	want := errors.New("socket configuration failed")
	conn := &keepAliveTestConn{err: want}
	if err := configureTCPKeepAlive(conn); !errors.Is(err, want) {
		t.Errorf("error = %v", err)
	}
	if conn.calls != 1 {
		t.Errorf("configured period after enable failed: %d calls", conn.calls)
	}
	conn = &keepAliveTestConn{periodErr: want}
	if err := configureTCPKeepAlive(conn); !errors.Is(err, want) {
		t.Errorf("period error = %v", err)
	}
	if conn.calls != 2 {
		t.Errorf("period failure: %d calls", conn.calls)
	}
}

func TestConfigureTCPKeepAliveNonTCP(t *testing.T) {
	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	if err := configureTCPKeepAlive(conn); err != nil {
		t.Fatal(err)
	}
}

// This checks Go API behavior on real sockets, without reading OS-specific
// socket options or relying on the timing of dropped keepalive packets.
func TestConfigureTCPKeepAliveClosedSocket(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
	peer, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := configureTCPKeepAlive(conn); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if err := configureTCPKeepAlive(conn); err == nil {
		t.Error("closed TCP socket configuration succeeded")
	}
	if err := configureTCPKeepAlive(tls.Server(conn, &tls.Config{})); err == nil {
		t.Error("closed underlying TLS socket configuration succeeded")
	}
}
