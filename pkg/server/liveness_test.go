package server

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// Record the installed deadline and synchronize with the decoder's reads.
// Applying the deadline is unnecessary: a nonzero deadline is already a failure.
type observedReadConn struct {
	net.Conn
	reads     chan struct{}
	mu        sync.Mutex
	deadlines []time.Time
}

func (c *observedReadConn) SetReadDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.deadlines = append(c.deadlines, deadline)
	c.mu.Unlock()
	return nil
}

func (c *observedReadConn) Read(p []byte) (int, error) {
	c.reads <- struct{}{}
	return c.Conn.Read(p)
}

func awaitLivenessSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("client operation did not finish")
	}
}

func newReadTestClient(t *testing.T, srv *Server) (*client, net.Conn, *observedReadConn, chan struct{}) {
	t.Helper()
	conn, peer := net.Pipe()
	observed := &observedReadConn{Conn: conn, reads: make(chan struct{}, 16)}
	log := logrus.New()
	log.SetOutput(io.Discard)
	srv.Log = log
	c := &client{conn: observed, recv: make(chan Message), readNext: make(chan struct{}),
		events: make(chan Message, 1), encoder: json.NewEncoder(observed), log: log}
	finished := make(chan struct{}, 2)
	go srv.readFromClient(c, finished)
	t.Cleanup(func() { peer.Close(); conn.Close() })
	return c, peer, observed, finished
}

func assertNoInactivityDeadline(t *testing.T, conn *observedReadConn) {
	t.Helper()
	conn.mu.Lock()
	defer conn.mu.Unlock()
	for _, deadline := range conn.deadlines {
		if !deadline.IsZero() {
			t.Errorf("installed application inactivity deadline: %v", deadline)
		}
	}
}

func TestIdleClientDoesNotRequirePingReply(t *testing.T) {
	srv := &Server{TimeBetweenPings: 5 * time.Millisecond, PingsUntilTimeout: 3}
	c, peer, observed, finished := newReadTestClient(t, srv)
	go srv.handleClient(c, finished)
	defer func() {
		peer.Close()
		awaitLivenessSignal(t, finished)
		awaitLivenessSignal(t, finished)
	}()
	awaitLivenessSignal(t, observed.reads)
	reader := bufio.NewReader(peer)
	peer.SetReadDeadline(time.Now().Add(5 * time.Second))
	// Exercise ping delivery beyond the former missed-ping limit without any
	// client-to-server messages. Deadline inspection makes this deterministic.
	for i := 0; i < 4; i++ {
		c.events <- pingMessage{}
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
	}
	assertNoInactivityDeadline(t, observed)
	if c.isStopped() {
		t.Fatal("idle client stopped without a ping reply")
	}
}

func TestReadLivenessSettings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		interval time.Duration
		count    int
	}{
		{"legacy timeout", 5 * time.Second, 3},
		{"timeout disabled", 5 * time.Second, 0},
		{"pings disabled", 0, 3},
		{"both disabled", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, peer, observed, finished := newReadTestClient(t, &Server{TimeBetweenPings: tc.interval, PingsUntilTimeout: tc.count})
			awaitLivenessSignal(t, observed.reads)
			assertNoInactivityDeadline(t, observed)
			peer.Close()
			awaitLivenessSignal(t, finished)
			if !c.isStopped() {
				t.Error("EOF did not stop client")
			}
		})
	}
}

func TestReadPreservesWhitespaceAndPartialMessages(t *testing.T) {
	c, peer, observed, finished := newReadTestClient(t, &Server{TimeBetweenPings: time.Nanosecond, PingsUntilTimeout: 1})
	awaitLivenessSignal(t, observed.reads)
	peer.SetWriteDeadline(time.Now().Add(5 * time.Second))
	for _, fragment := range []string{"\n \t", `{"type":"protocol_`, `version","version":2}`} {
		if _, err := io.WriteString(peer, fragment); err != nil {
			t.Fatal(err)
		}
		if fragment != `version","version":2}` {
			awaitLivenessSignal(t, observed.reads)
		}
	}
	select {
	case msg := <-c.recv:
		if msg.Name() != "protocol_version" {
			t.Fatalf("message = %q", msg.Name())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("partial message was lost")
	}
	c.readNext <- struct{}{}
	awaitLivenessSignal(t, observed.reads)
	assertNoInactivityDeadline(t, observed)
	peer.Close()
	awaitLivenessSignal(t, finished)
}

func TestStopUnblocksIdleRead(t *testing.T) {
	c, _, observed, finished := newReadTestClient(t, &Server{})
	awaitLivenessSignal(t, observed.reads)
	c.stop("test shutdown")
	awaitLivenessSignal(t, finished)
	if c.stopReason != "test shutdown" {
		t.Errorf("stop reason = %q", c.stopReason)
	}
}

type disconnectedHook struct{ done chan struct{} }

func (h *disconnectedHook) Levels() []logrus.Level { return logrus.AllLevels }
func (h *disconnectedHook) Fire(entry *logrus.Entry) error {
	if entry.Message == "Client disconnected" {
		close(h.done)
	}
	return nil
}

func TestDisconnectedClientLeavesRegistry(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "EOF", true: "send error while read is idle"}[failWrite], func(t *testing.T) {
			conn, peer := net.Pipe()
			defer conn.Close()
			defer peer.Close()
			log := logrus.New()
			log.SetOutput(io.Discard)
			hook := &disconnectedHook{done: make(chan struct{})}
			log.AddHook(hook)
			srv := &Server{Log: log, registry: registry{clients: make(map[uint64]channelMember), channels: make(map[string]*channel)}}
			srv.serveClient(conn, 7, "test peer")
			peer.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err := io.WriteString(peer, "{\"type\":\"join\",\"channel\":\"test\",\"connection_type\":\"master\"}\n"); err != nil {
				t.Fatal(err)
			}
			var response ClientChannelJoinedResponse
			if err := json.NewDecoder(peer).Decode(&response); err != nil {
				t.Fatal(err)
			}
			if response.Type != "channel_joined" {
				t.Fatalf("response = %#v", response)
			}
			if failWrite {
				// Fail a server event write while its reader is blocked on an idle
				// connection. This must wake the reader so both goroutines exit.
				conn.SetWriteDeadline(time.Now().Add(-time.Second))
				srv.registry.lock.RLock()
				events := srv.registry.clients[7].events
				srv.registry.lock.RUnlock()
				events <- pingMessage{}
			} else {
				peer.Close()
			}
			awaitLivenessSignal(t, hook.done)
			// Channel removal acknowledges leave before deleting registry entries.
			// Wait for that asynchronous cleanup without assuming scheduler order.
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			timeout := time.NewTimer(5 * time.Second)
			defer timeout.Stop()
			for {
				srv.registry.lock.RLock()
				empty := len(srv.registry.clients) == 0 && len(srv.registry.channels) == 0
				srv.registry.lock.RUnlock()
				if empty {
					break
				}
				select {
				case <-ticker.C:
				case <-timeout.C:
					t.Fatal("disconnected client remains in registry")
				}
			}
		})
	}
}
