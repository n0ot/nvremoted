package server

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// runClientEvents sends the same internal events used by the periodic timer and
// channels through handleClient, and records what the peer receives. Injecting
// events avoids wall-clock sleeps and the server's non-terminating Serve loop.
func runClientEvents(t *testing.T, events ...Message) ([]string, *client) {
	t.Helper()
	conn, peer := net.Pipe()
	log := logrus.New()
	log.SetOutput(io.Discard)
	c := &client{
		conn:    conn,
		events:  make(chan Message, 1),
		recv:    make(chan Message),
		encoder: json.NewEncoder(conn),
		log:     log,
	}
	finished := make(chan struct{}, 1)
	srv := &Server{}
	go srv.handleClient(c, finished)
	recvClosed := false
	handlerFinished := false
	defer func() {
		peer.Close()
		conn.Close()
		if !recvClosed {
			close(c.recv)
		}
		if !handlerFinished {
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Error("client handler did not finish during cleanup")
			}
		}
	}()
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := peer.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(peer)
	var lines []string
	for _, event := range events {
		select {
		case c.events <- event:
		case <-time.After(5 * time.Second):
			t.Fatal("client handler did not receive event")
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading event %q response: %v", event.Name(), err)
		}
		lines = append(lines, line)
	}
	close(c.recv)
	recvClosed = true
	select {
	case <-finished:
		handlerFinished = true
	case <-time.After(5 * time.Second):
		t.Fatal("client handler did not finish")
	}
	return lines, c
}

func TestHandleClientChannelEvents(t *testing.T) {
	member := channelMember{id: 7, connectionType: "master"}
	for _, tc := range []struct {
		name  string
		event Message
		wire  string
	}{
		{"message", channelMessage{origin: 7, msg: map[string]interface{}{"type": "speak", "text": "hello"}}, "{\"origin\":7,\"text\":\"hello\",\"type\":\"speak\"}\n"},
		{"join", joinedChannelMSG(member), "{\"type\":\"client_joined\",\"client\":{\"type\":\"client\",\"id\":7,\"connection_type\":\"master\"}}\n"},
		{"leave", leftChannelMSG(member), "{\"type\":\"client_left\",\"client\":{\"type\":\"client\",\"id\":7,\"connection_type\":\"master\"}}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines, c := runClientEvents(t, tc.event)
			if lines[0] != tc.wire {
				t.Errorf("response = %q, want %q", lines[0], tc.wire)
			}
			if c.isStopped() {
				t.Errorf("channel event stopped client: %s", c.stopReason)
			}
		})
	}
}

func TestHandleClientUnknownEvent(t *testing.T) {
	// GenericClientMessage is not a registered internal event.
	lines, c := runClientEvents(t, GenericClientMessage{})
	want := "{\"type\":\"error\",\"error\":\"internal error\"}\n"
	if lines[0] != want {
		t.Errorf("response = %q, want %q", lines[0], want)
	}
	if !c.isStopped() || c.stopReason != "internal error" {
		t.Errorf("unknown event: stopped = %v, reason = %q", c.isStopped(), c.stopReason)
	}
}

func TestHandleClientPing(t *testing.T) {
	lines, c := runClientEvents(t, pingMessage{})
	want := "{\"type\":\"ping\"}\n"
	if lines[0] != want {
		t.Errorf("ping response = %q, want %q", lines[0], want)
	}
	if c.isStopped() {
		t.Errorf("ping stopped client: %s", c.stopReason)
	}
}

func TestHandleClientChannelEventsAroundPings(t *testing.T) {
	event := channelMessage{origin: 7, msg: map[string]interface{}{"type": "speak", "text": "hello"}}
	lines, c := runClientEvents(t, event, pingMessage{}, event, pingMessage{}, event)
	wantChannel := "{\"origin\":7,\"text\":\"hello\",\"type\":\"speak\"}\n"
	for i, line := range lines {
		want := wantChannel
		if i%2 == 1 {
			want = "{\"type\":\"ping\"}\n"
		}
		if line != want {
			t.Errorf("response %d = %q, want %q", i, line, want)
		}
	}
	if c.isStopped() {
		t.Errorf("events around pings stopped client: %s", c.stopReason)
	}
}
