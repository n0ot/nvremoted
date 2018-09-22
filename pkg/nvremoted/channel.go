package nvremoted

import (
	"errors"
	"strings"
	"sync"

	"github.com/n0ot/nvremoted/pkg/model"
)

// A channel relays traffic between clients using the same key.
type channel struct {
	name       string
	membersMTX sync.RWMutex // Protects members
	members    []*channelMember
}

// newChannel makes a new channel.
func newChannel(name string) *channel {
	return &channel{
		name: name,
		// Assume a new channel is being made because at least one client wants to join it.
		members: make([]*channelMember, 0, 1),
	}
}

// Broadcast broadcasts a message to all members of this channel.
// Members whose ID is in excludeIDs will be skipped.
func (ch *channel) Broadcast(msg model.Message, excludeIDs ...uint64) {
	ch.membersMTX.RLock()
	defer ch.membersMTX.RUnlock()
	ch.broadcast(msg, excludeIDs...)
}

func (ch *channel) broadcast(msg model.Message, excludeIDs ...uint64) {
	excludedIDSet := make(map[uint64]struct{})
	for _, id := range excludeIDs {
		excludedIDSet[id] = struct{}{}
	}

	for _, member := range ch.members {
		if _, idExcluded := excludedIDSet[member.ID]; idExcluded {
			continue
		}
		member.Send <- msg
	}
}

// Join adds a member to this channel, and sends all other members a client_joined message.
// If the member is already in this channel, an error will be returned.
// The joining member will be sent a channel_joined message.
func (ch *channel) Join(c *Client, connectionType string) error {
	ch.membersMTX.Lock()
	defer ch.membersMTX.Unlock()
	for i := 0; i < len(ch.members); i++ {
		if c.ID == ch.members[i].ID {
			return errors.New("Already in channel")
		}
	}

	member := &channelMember{
		Client:         c,
		ConnectionType: connectionType,
	}
	ch.broadcast(model.Message{
		"type":   "client_joined",
		"client": member,
	})

	c.Send <- model.Message{
		"origin":  member.ID,
		"clients": ch.members,
		"type":    "channel_joined",
		"channel": ch.name,
	}
	ch.members = append(ch.members, member)

	return nil
}

// Leave removes a member from this channel, and sends all other members a client_left message.
// If the member's ID isn't in the channel, Leave will return an error.
// If there are still members in the channel after memberID was removed, more will be true.
func (ch *channel) Leave(id uint64, reason string) (more bool, err error) {
	var member *channelMember
	ch.membersMTX.Lock()
	for i := 0; i < len(ch.members); i++ {
		if id != ch.members[i].ID {
			continue
		}
		member = ch.members[i]
		ch.members = append(ch.members[:i], ch.members[i+1:]...)
		break
	}
	ch.membersMTX.Unlock()

	ch.membersMTX.RLock()
	more = len(ch.members) != 0
	ch.membersMTX.RUnlock()
	if member == nil {
		return more, errors.New("Member ID not in channel")
	}

	msg := model.Message{
		"type":   "client_left",
		"client": member,
	}
	if reason != "" {
		msg["reason"] = reason
	}

	ch.broadcast(msg)
	return
}

// encrypted returns whether the channel is carrying encrypted messages.
func (ch *channel) encrypted() bool {
	return strings.HasPrefix(ch.name, "E2E_") && len(ch.name) == 68
}

// A channelMember holds a client's state on a channel.
type channelMember struct {
	*Client
	ConnectionType string `json:"connection_type"`
}
