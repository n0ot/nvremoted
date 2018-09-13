package nvremoted

// channel relays traffic between clients using the same key.
type channel struct {
	name    string
	members []*channelMember
}

// channelMember holds a client's state on a channel.
type channelMember struct {
	ID             uint64 `json:"id"`
	ConnectionType string `json:"connection_type"`
}
