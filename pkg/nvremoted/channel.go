package nvremoted

import (
	"errors"
	"strings"

	"github.com/n0ot/nvremoted/pkg/model"
)

var errAlreadyInChannel = errors.New("Already in channel")

// A channel relays traffic between clients using the same key.
type channel struct {
	name    string
	members []*channelMember
}

// newChannel makes a new channel.
func newChannel(name string) *channel {
	return &channel{
		name: name,
		// Assume a new channel is being made because at least one client wants to join it.
		members: make([]*channelMember, 0, 1),
	}
}

// broadcast broadcasts a message to all members of this channel.
// Members whose ID is in excludeIDs will be skipped.
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

// join adds a member to this channel, and sends all other members a client_joined message.
// If the member is already in this channel, an error will be returned.
// The joining member will be sent a channel_joined message.
func (ch *channel) join(c *Client, connectionType string) error {
	for i := 0; i < len(ch.members); i++ {
		if c.ID == ch.members[i].ID {
			return errAlreadyInChannel
		}
	}

	member := &channelMember{
		Client:         c,
		ConnectionType: connectionType,
	}
	ch.broadcast(newClientJoinedMessage(member))
	c.Send <- newChannelJoinedMessage(member.ID, ch)
	ch.members = append(ch.members, member)

	return nil
}

// leave removes a member from this channel, and sends all other members a client_left message.
// If the member's ID isn't in the channel, Leave will return an error.
// If there are still members in the channel after memberID was removed, more will be true.
func (ch *channel) leave(id uint64, reason string) (more bool, err error) {
	var member *channelMember
	for i := 0; i < len(ch.members); i++ {
		if id != ch.members[i].ID {
			continue
		}
		member = ch.members[i]
		ch.members = append(ch.members[:i], ch.members[i+1:]...)
		break
	}

	more = len(ch.members) != 0
	if member == nil {
		return more, errors.New("Member ID not in channel")
	}

	ch.broadcast(newClientLeftMessage(member, reason))
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

// A channelMessage can contain random fields.
type ChannelMessage map[string]interface{}

// Type gets the type of this ChannelMessage.
// If this ChannelMessage has no type, Type will return an empty string.
func (msg ChannelMessage) Message() string {
	t, _ := msg["type"].(string)
	return t
}

type channelJoinedMessage struct {
	model.DefaultMessage
	Origin  uint64           `json:"origin"`
	Clients []*channelMember `json:"clients"`
	Channel string           `json:"channel"`
}

func newChannelJoinedMessage(id uint64, ch *channel) channelJoinedMessage {
	msg := channelJoinedMessage{
		DefaultMessage: model.DefaultMessage{"channel_joined"},
		Origin:         id,
	}
	if ch != nil {
		msg.Clients = ch.members
		msg.Channel = ch.name
	}
	return msg
}

type clientJoinedMessage struct {
	model.DefaultMessage
	Client *channelMember `json:"client"`
}

func newClientJoinedMessage(member *channelMember) clientJoinedMessage {
	return clientJoinedMessage{
		DefaultMessage: model.DefaultMessage{"client_joined"},
		Client:         member,
	}
}

type clientLeftMessage struct {
	model.DefaultMessage
	Client *channelMember `json:"client"`
	Reason string         `json:"reason,omitempty"`
}

func newClientLeftMessage(member *channelMember, reason string) clientLeftMessage {
	return clientLeftMessage{
		DefaultMessage: model.DefaultMessage{"client_left"},
		Client:         member,
		Reason:         reason,
	}
}
