package server

import (
	"errors"
	"strings"
	"sync"
	"time"
)

type channel struct {
	name    string
	members []channelMember

	// messages receives messages to be broadcast over the channel.
	messages chan channelMessage
	// joins receives members to add to the channel
	joins chan joinChannelRequest
	// parts receives member IDs to remove from the channel
	// If there are no more members, and no pending joins, the channel will be destroyed.
	parts chan leaveChannelRequest

	pendingJoinsLock sync.Mutex // Protects pendingJoins
	// pendingJoins is the number of clients who have fetched this channel from the registry, but have not yet joined
	pendingJoins int
}

type channelMember struct {
	id             uint64
	connectionType string
	events         chan<- Message
}

type joinChannelRequest struct {
	member channelMember
	resp   chan interface{} // response could either be a list of existing members or an error
}

// joinChannel adds a member to the named channel, creating it if it doesn't already exist.
func joinChannel(name string, member channelMember, reg *registry) (*channel, []channelMember, error) {
	reg.lock.Lock()
	reg.clients[member.id] = member
	if len(reg.clients) > reg.maxClients {
		reg.maxClients = len(reg.clients)
		reg.maxClientsTime = time.Now()
	}

	c, ok := reg.channels[name]
	if !ok {
		c = &channel{
			name:     name,
			members:  []channelMember{},
			messages: make(chan channelMessage),
			joins:    make(chan joinChannelRequest),
			parts:    make(chan leaveChannelRequest),
		}
		reg.channels[name] = c
		go c.start(reg)

		if c.isE2e() {
			reg.numE2eChannels++
		}
		if len(reg.channels) > reg.maxChannels {
			reg.maxChannels = len(reg.channels)
			reg.maxChannelsTime = time.Now()
		}
	}

	// We don't want to join the channel while the registry is locked, because slow channel goroutines will bog it down for everyone.
	// But we do need to note that there is a join pending, so that if the channel becomes empty before this member joins,
	// it doesn't spin down and remove itself from the registry.
	c.pendingJoinsLock.Lock()
	c.pendingJoins++
	c.pendingJoinsLock.Unlock()
	reg.lock.Unlock()
	// Join the channel, now that the registry is unlocked
	req := joinChannelRequest{
		member: member,
		resp:   make(chan interface{}),
	}
	c.joins <- req

	switch result := (<-req.resp).(type) {
	case error:
		return c, nil, result
	case []channelMember:
		return c, result, nil
	}

	return c, nil, errors.New("Received unknown type from channel")
}

type leaveChannelRequest struct {
	id   uint64
	resp chan struct{}
}

// leave removes a member from the channel, destroying the channel if it is empty.
func (c *channel) leave(id uint64) {
	req := leaveChannelRequest{
		id:   id,
		resp: make(chan struct{}),
	}
	c.parts <- req
	<-req.resp
}

func (c *channel) start(reg *registry) {
	for {
		select {
		case req := <-c.joins:
			var exists bool
			for _, member := range c.members {
				if req.member.id == member.id {
					exists = true
					break // Already in the channel
				}
			}

			if !exists {
				// Send current members to the joiner
				// and notify existing members.
				req.resp <- c.members
				c.broadcast(joinedChannelMSG(req.member))
				c.members = append(c.members, req.member)
			} else {
				req.resp <- errors.New("already a member")
			}
			c.pendingJoinsLock.Lock()
			c.pendingJoins--
			c.pendingJoinsLock.Unlock()

		case req := <-c.parts:
			for i, member := range c.members {
				if req.id == member.id {
					c.members = append(c.members[:i], c.members[i+1:]...)
					c.broadcast(leftChannelMSG(member))
				}
			}
			// Tell the requester the removal is complete.
			// This does not mean a member was actually removed, if the specified ID wasn't already in the channel.
			req.resp <- struct{}{}

			reg.lock.Lock()
			delete(reg.clients, req.id)
			// Destroy the channel if there are no more members and no more pending joins
			c.pendingJoinsLock.Lock()
			if len(c.members) == 0 && c.pendingJoins == 0 {
				delete(reg.channels, c.name)
				if c.isE2e() {
					reg.numE2eChannels--
				}
				c.pendingJoinsLock.Unlock()
				reg.lock.Unlock()
				return
			}
			c.pendingJoinsLock.Unlock()
			reg.lock.Unlock()

		case msg := <-c.messages:
			for _, member := range c.members {
				if msg.origin != member.id {
					member.events <- msg
				}
			}

		}
	}
}

func (c *channel) broadcast(msg Message) {
	for _, member := range c.members {
		member.events <- msg
	}
}

func (c *channel) isE2e() bool {
	return strings.HasPrefix(c.name, "E2E_") && len(c.name) == 68
}

type joinedChannelMSG channelMember

func (joinedChannelMSG) Name() string {
	return "joined_channel"
}

type leftChannelMSG channelMember

func (leftChannelMSG) Name() string {
	return "left_channel"
}

type channelMessage struct {
	origin uint64
	msg    map[string]interface{}
}

func (channelMessage) Name() string {
	return "channel_message"
}
