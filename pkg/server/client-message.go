package server

import "time"

var clientMessages map[string]func() Message
var clientMessageHandlers map[string]clientMessageHandlerFunc
var clientEventHandlers map[string]clientEventHandlerFunc

type clientMessageHandlerFunc func(*client, Message)
type clientEventHandlerFunc func(*client, Message)

// GenericClientMessage holds a message's "type", which is included in every message sent from a client.
type GenericClientMessage struct {
	Type string `json:"type"`
}

// Name gets this GenericClientMessage's name.
func (msg GenericClientMessage) Name() string {
	return "generic"
}

// ClientResponse is a generic response to be sent to a client.
type ClientResponse map[string]interface{}

// Name gets this ClientResponse's name.
func (ClientResponse) Name() string {
	return "response"
}

// GenericClientResponse holds a response's "type", which is included in every message sent to a client.
type GenericClientResponse struct {
	Type string `json:"type"`
}

// Name gets this GenericClientResponse's name.
func (msg GenericClientResponse) Name() string {
	return "generic"
}

// ClientErrorResponse contains an error to be sent to a client.
type ClientErrorResponse struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

// Name gets this ClientErrorResponse's name.
func (ClientErrorResponse) Name() string {
	return "error"
}

// ClientMemberResponse contains channel member information about another client.
type ClientMemberResponse struct {
	Type           string `json:"type"`
	ID             uint64 `json:"id"`
	ConnectionType string `json:"connection_type"`
}

// Name gets this ClientMemberResponse's name.
func (ClientMemberResponse) Name() string {
	return "client"
}

func clientMemberResponseFromChannelMember(member channelMember) ClientMemberResponse {
	return ClientMemberResponse{
		Type:           "client",
		ID:             member.id,
		ConnectionType: member.connectionType,
	}
}

// ClientChannelJoinedResponse is sent to clients when they join a channel.
type ClientChannelJoinedResponse struct {
	Type    string                 `json:"type"`
	Clients []ClientMemberResponse `json:"clients"`
	Channel string                 `json:"channel"`
	Origin  uint64                 `json:"origin"`
}

// Name gets this ClientChannelJoinedResponse's name.
func (ClientChannelJoinedResponse) Name() string {
	return "channel_joined"
}

// ClientClientJoinedResponse is sent to existing members of a channel when a new client joins.
type ClientClientJoinedResponse struct {
	Type   string               `json:"type"`
	Client ClientMemberResponse `json:"client"`
}

// Name gets this ClientClientJoinedResponse's name.
func (ClientClientJoinedResponse) Name() string {
	return "client_joined"
}

// ClientClientLeftResponse is sent to remaining members of a channel when a client leaves.
type ClientClientLeftResponse struct {
	Type   string               `json:"type"`
	Client ClientMemberResponse `json:"client"`
}

// Name gets this ClientClientLeftResponse's name.
func (ClientClientLeftResponse) Name() string {
	return "client_left"
}

// ClientStatsResponse contains information about the running state of NVRemoted.
type ClientStatsResponse struct {
	Type  string `json:"type"`
	Stats Stats  `json:"stats"`
}

// Name gets this ClientStatsResponse's name.
func (ClientStatsResponse) Name() string {
	return "stats"
}

// ClientMOTDResponse contains the message of the day, and is sent to connecting clients.
type ClientMOTDResponse struct {
	Type         string `json:"type"`
	MOTD         string `json:"motd"`
	ForceDisplay bool   `json:"force_display"`
}

// Name gets this ClientMOTDResponse's name.
func (ClientMOTDResponse) Name() string {
	return "motd"
}

func init() {
	clientMessages = make(map[string]func() Message)
	clientMessageHandlers = make(map[string]clientMessageHandlerFunc)
	clientEventHandlers = make(map[string]clientEventHandlerFunc)

	clientMessages["join"] = func() Message {
		return &ClientJoinMessage{}
	}
	clientMessageHandlers["join"] = handleClientJoin

	clientMessages["protocol_version"] = func() Message {
		return &ClientProtocolVersionMessage{}
	}
	clientMessageHandlers["protocol_version"] = handleClientProtocolVersion

	clientMessageHandlers["channel_message"] = handleClientChannelMessage

	clientMessages["stat"] = func() Message {
		return &ClientStatMessage{}
	}
	clientMessageHandlers["stat"] = handleClientStatMessage

	clientEventHandlers["channel_message"] = handleClientChannelEvent
	clientEventHandlers["joined_channel"] = handleClientJoinEvent
	clientEventHandlers["left_channel"] = handleClientLeaveEvent
}

// ClientProtocolVersionMessage contains the protocol version sent by a client.
type ClientProtocolVersionMessage struct {
	GenericClientMessage
	Version int `json:"version"`
}

// Name gets this ClientProtocolVersionMessage's name.
func (ClientProtocolVersionMessage) Name() string {
	return "protocol_version"
}

func handleClientProtocolVersion(c *client, msg Message) {
	protvMSG := msg.(*ClientProtocolVersionMessage)
	// Only version 2 is supported for now;
	// allow clients to continue without providing a version, but kick those who provide a version that isn't 2.
	if protvMSG.Version != 2 {
		c.sendError("version unsupported")
		c.stop("protocol version unsupported")
	}
}

// ClientJoinMessage is received when a client wishes to join a channel.
type ClientJoinMessage struct {
	GenericClientMessage
	Channel        string `json:"channel"`
	ConnectionType string `json:"connection_type"`
}

// Name gets this ClientJoinMessage's name.
func (ClientJoinMessage) Name() string {
	return "join"
}

func handleClientJoin(c *client, msg Message) {
	joinMSG := msg.(*ClientJoinMessage)
	if joinMSG.Channel == "" {
		c.sendError("no channel specified")
		c.stop("protocol error")
		return
	}
	if joinMSG.ConnectionType == "" {
		c.sendError("no connection_type specified")
		c.stop("protocol error")
		return
	}
	if c.channel != nil {
		c.sendError("already in a channel")
		c.stop("protocol error")
		return
	}

	member := channelMember{
		id:             c.id,
		connectionType: joinMSG.ConnectionType,
		events:         c.events,
	}

	if ch, members, err := joinChannel(joinMSG.Channel, member, c.registry); err != nil {
		c.sendError(err.Error())
		c.stop("protocol error")
	} else {
		memberResponses := []ClientMemberResponse{}
		for _, member := range members {
			memberResponses = append(memberResponses, clientMemberResponseFromChannelMember(member))
		}
		c.send(ClientChannelJoinedResponse{
			Type:    "channel_joined",
			Clients: memberResponses,
			Channel: joinMSG.Channel,
			Origin:  c.id,
		})
		c.channel = ch
	}
}

// ClientStatMessage is sent by clients requesting server stats.
type ClientStatMessage struct {
	GenericClientMessage
	Password string `json:"password"`
}

// Name gets this ClientStatMessage's name.
func (ClientStatMessage) Name() string {
	return "stat"
}

func handleClientStatMessage(c *client, msg Message) {
	statReq := msg.(*ClientStatMessage)

	if c.channel != nil {
		c.sendError("no stats while in channel")
		c.stop("protocol error")
		return
	}
	if statReq.Password == "" {
		c.sendError("no password")
		c.stop("no stats password provided")
		return
	}
	if c.registry.statsPassword != statReq.Password {
		time.Sleep(5 * time.Second) // Prevent broot forcing
		c.sendError("wrong password")
		c.stop("wrong stats password")
		return
	}

	c.send(ClientStatsResponse{
		Type:  "stats",
		Stats: c.registry.Stats(),
	})
	c.stop("stats request completed")
}

func handleClientChannelMessage(c *client, msg Message) {
	channelMSG := msg.(*channelMessage)
	if c.channel == nil {
		c.sendError("not in a channel")
		c.stop("protocol error")
		return
	}

	c.channel.messages <- *channelMSG
}

func handleClientChannelEvent(c *client, msg Message) {
	channelMSG := msg.(channelMessage)
	resp := make(ClientResponse)

	for k, v := range channelMSG.msg {
		resp[k] = v
	}
	resp["origin"] = channelMSG.origin
	c.send(resp)
}

func handleClientJoinEvent(c *client, msg Message) {
	member := channelMember(msg.(joinedChannelMSG))
	c.send(ClientClientJoinedResponse{
		Type:   "client_joined",
		Client: clientMemberResponseFromChannelMember(member),
	})
}

func handleClientLeaveEvent(c *client, msg Message) {
	member := channelMember(msg.(leftChannelMSG))
	c.send(ClientClientLeftResponse{
		Type:   "client_left",
		Client: clientMemberResponseFromChannelMember(member),
	})
}
