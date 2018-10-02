package nvremoted

import (
	"os"
	"reflect"
	"testing"

	"github.com/n0ot/nvremoted/pkg/model"
	"github.com/sirupsen/logrus"
)

var log *logrus.Logger

func init() {
	log := logrus.New()
	log.Out = os.Stderr
	log.Level = logrus.DebugLevel
}

func TestJoinChannel(t *testing.T) {
	nvrd := New(log, "", false)

	// Test that joining clients are notified,
	// added to the channel,
	// and that existing clients are notified.
	clients := []*Client{}
	members := []*channelMember{}
	memberChans := []<-chan model.Message{}
	for i := 0; i < 10; i++ {
		c := make(chan model.Message)
		client := &Client{
			ID:   uint64(i),
			Send: c,
		}
		clients = append(clients, client)
		members = append(members, &channelMember{client, "master"})
		memberChans = append(memberChans, c)
		nvrd.AddClient(client)
	}

	for i := range clients {
		testJoinChannelHelper(t, nvrd, "test-chan", clients[i], memberChans[i], "master", members[:i], memberChans[:i])
	}
}

func TestSwitchChannels(t *testing.T) {
	nvrd := New(log, "", false)

	c := make(chan model.Message)
	client := &Client{
		ID:   1,
		Send: c,
	}

	go func() {
		<-c
		<-c
	}()

	if err := nvrd.AddClient(client); err != nil {
		t.Errorf("Cannot add client")
	}
	if err := nvrd.JoinChannel("test-chan1", "master", client.ID); err != nil {
		t.Fatalf("Cannot join channel: %s", err)
	}
	if err := nvrd.JoinChannel("test-chan1", "master", client.ID); err != nil {
		t.Fatalf("Cannot switch channels: %s", err)
	}
}

func testJoinChannelHelper(t *testing.T, nvrd *NVRD, channelName string, client *Client, clientChan <-chan model.Message, connectionType string, existingMembers []*channelMember, memberChans []<-chan model.Message) {
	member := &channelMember{
		Client:         client,
		ConnectionType: connectionType,
	}

	go func() {
		gotMSG := <-clientChan
		wantedMSG := channelJoinedMessage{
			Origin:         client.ID,
			Clients:        existingMembers,
			DefaultMessage: model.DefaultMessage{"channel_joined"},
			Channel:        channelName,
		}
		if !reflect.DeepEqual(wantedMSG, gotMSG) {
			t.Errorf("Invalid channel_joined; wanted %+v, got: %+v", wantedMSG, gotMSG)
		}
	}()

	go func() {
		for _, mChan := range memberChans {
			gotMSG := <-mChan
			wantedMSG := clientJoinedMessage{
				Client:         member,
				DefaultMessage: model.DefaultMessage{"client_joined"},
			}
			if !reflect.DeepEqual(wantedMSG, gotMSG) {
				t.Errorf("Invalid client_joined; wanted %+v, got: %+v", wantedMSG, gotMSG)
			}
		}
	}()

	if err := nvrd.JoinChannel(channelName, connectionType, client.ID); err != nil {
		t.Fatalf("Joining channel: %s", err)
	}
}

func TestLeaveChannel(t *testing.T) {
	channelName := "test-chan"
	nvrd := New(log, "", false)

	c1 := make(chan model.Message)
	client1 := &Client{
		ID:   1,
		Send: c1,
	}
	c2 := make(chan model.Message)
	client2 := &Client{
		ID:   2,
		Send: c2,
	}

	// c1 receives a channel_joined and a client_joined.
	go func() {
		<-c1
		<-c1
	}()
	// c2 receives a channel_joined.
	go func() {
		<-c2
	}()

	var err error
	err = nvrd.AddClient(client1)
	err = nvrd.AddClient(client2)
	err = nvrd.JoinChannel(channelName, "master", client1.ID)
	err = nvrd.JoinChannel(channelName, "slave", client2.ID)
	if err != nil {
		t.Fatalf("Join channel: %s", err)
	}

	go func() {
		gotMSG := <-c1
		wantedMSG := clientLeftMessage{
			DefaultMessage: model.DefaultMessage{"client_left"},
			Client: &channelMember{
				Client:         client2,
				ConnectionType: "slave",
			},
			Reason: "Quit",
		}
		if !reflect.DeepEqual(wantedMSG, gotMSG) {
			t.Errorf("Leaving channel; wanted: %+v; got: %+v", wantedMSG, gotMSG)
		}
	}()

	err = nvrd.LeaveChannel(client2.ID, "Quit")
	if err != nil {
		t.Errorf("Error leaving channel: %s", err)
	}

	err = nvrd.LeaveChannel(client1.ID, "")
	if err != nil {
		t.Errorf("Error leaving channel: %s", err)
	}
}
