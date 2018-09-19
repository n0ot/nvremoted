// Package channels contains behavior for channels on an NVRemoted server.

package channels

import (
	"errors"

	"github.com/n0ot/nvremoted/pkg/models"
)

// Channel relays traffic between clients using the same key.
type Channel struct {
	Name    string
	members []*channelMember
}

// New makes a new channel.
func New(name string) *Channel {
	return &Channel{
		Name:    name,
		members: make([]*channelMember, 0),
	}
}

// Broadcast broadcasts a message to all members of this channel.
// Members whose ID is in excludeIDs will be skipped.
func (ch *Channel) Broadcast(msg models.Message, excludeIDs ...uint64) {
	excludedIDSet := make(map[uint64]struct{})
	for _, id := range excludeIDs {
		excludedIDSet[id] = struct{}{}
	}

	for _, member := range ch.members {
		if _, idExcluded := excludedIDSet[member.ID]; idExcluded || member.resp == nil {
			continue
		}
		member.resp <- msg
	}
}

// Join adds a member to this channel, and sends all other members a client_joined message.
// If the member is already in this channel, an error will be returned.
// The joining member will be sent a channel_joined message.
func (ch *Channel) Join(memberID uint64, connectionType string, resp chan<- models.Message) error {
	for i := 0; i < len(ch.members); i++ {
		if memberID == ch.members[i].ID {
			return errors.New("Already in channel")
		}
	}

	member := &channelMember{
		ID:             memberID,
		ConnectionType: connectionType,
	}
	ch.Broadcast(models.Message(map[string]interface{}{
		"type":   "client_joined",
		"client": member,
	}))
	ch.members = append(ch.members, member)

	resp <- models.Message(map[string]interface{}{
		"origin":  memberID,
		"clients": ch.members,
		"type":    "channel_joined",
		"channel": ch.Name,
	})

	return nil
}

// Leave removes a member from this channel, and sends all other members a client_left message.
// If the memberID isn't in the channel, Leave will return an error.
// If there are still members in the channel after memberID was removed, more will be true.
func (ch *Channel) Leave(memberID uint64, reason string) (more bool, err error) {
	var member *channelMember
	for i := 0; i < len(ch.members); i++ {
		if memberID != ch.members[i].ID {
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

	msg := models.Message(map[string]interface{}{
		"type":   "client_left",
		"client": member,
	})
	if reason != "" {
		msg["reason"] = reason
	}

	ch.Broadcast(msg)
	return
}
