package channels

import "github.com/n0ot/nvremoted/pkg/models"

// member holds a client's state on a channel.
type member struct {
	ID             uint64 `json:"id"`
	ConnectionType string `json:"connection_type"`
	resp           chan<- models.Message
}
