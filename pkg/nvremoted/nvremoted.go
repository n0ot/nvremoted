// Package nvremoted provides services to NVDA Remote clients.
package nvremoted

import (
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/n0ot/nvremoted/pkg/model"
)

// NVRD Contains state for an NVRemoted service.
type NVRD struct {
	log                    *logrus.Logger
	startedAt              time.Time
	channelsMTX            sync.RWMutex // Protects channels
	channels               map[string]*channel
	maxChannels            int
	maxChannelsTime        time.Time
	numUnencryptedChannels int
	clientsMTX             sync.RWMutex // Protects clients
	clients                map[uint64]*Client
	maxClients             int
	maxClientsTime         time.Time
	numUnencryptedClients  int
	MOTD                   string
	WarnIfNotEncrypted     bool
}

// New creates a new NVRemoted service.
func New(log *logrus.Logger, MOTD string, WarnIfNotEncrypted bool) *NVRD {
	return &NVRD{
		log:                log,
		startedAt:          time.Now(),
		channels:           make(map[string]*channel),
		maxChannelsTime:    time.Now(),
		maxClientsTime:     time.Now(),
		clients:            make(map[uint64]*Client),
		MOTD:               MOTD,
		WarnIfNotEncrypted: WarnIfNotEncrypted,
	}
}

// A Client identifies a consumer of the NVRemoted service.
type Client struct {
	ID             uint64               `json:"id"`
	Send           chan<- model.Message `json:"-"`
	ConnectedSince time.Time            `json:"connected_since"`
	ch             *channel
}

// AddClient adds a client to NVRemoted.
func (n *NVRD) AddClient(c *Client) error {
	n.clientsMTX.Lock()
	if _, ok := n.clients[c.ID]; ok {
		n.clientsMTX.Unlock()
		return errors.New("Client already exists")
	}
	n.clients[c.ID] = c
	if len(n.clients) > n.maxClients {
		n.maxClients = len(n.clients)
		n.maxClientsTime = time.Now()
	}
	n.clientsMTX.Unlock()

	if n.MOTD != "" {
		c.Send <- newMOTDMessage(n.MOTD, false)
	}
	return nil
}

// JoinChannel adds a client to a channel.
// If the client is in another channel,
// they will be removed from the other channel first.
func (n *NVRD) JoinChannel(name, connectionType string, id uint64) error {
	n.clientsMTX.Lock()
	defer n.clientsMTX.Unlock()
	n.channelsMTX.Lock()
	defer n.channelsMTX.Unlock()
	c, ok := n.clients[id]
	if !ok {
		return errors.New("Client not found")
	}

	if c.ch != nil {
		if err := n.leaveChannel(id, "Client switched channels"); err != nil {
			return errors.Wrap(err, "Join channel")
		}
	}

	ch, existing := n.channels[name]
	if !existing {
		ch = newChannel(name)
	}
	if err := ch.join(c, connectionType); err != nil {
		return err
	}
	c.ch = ch
	chEncrypted := ch.encrypted()
	if !existing {
		n.channels[name] = ch
		if len(n.channels) > n.maxChannels {
			n.maxChannels = len(n.channels)
			n.maxChannelsTime = time.Now()
		}
		if !chEncrypted {
			n.numUnencryptedChannels++
		}
	}

	if !chEncrypted {
		n.numUnencryptedClients++
		if n.WarnIfNotEncrypted {
			c.Send <- newMOTDMessage("Your traffic will pass through this server unencrypted. Please consider upgrading to a version of NVDA Remote that supports end to end encryption.", true)
		}
	}

	return nil
}

// LeaveChannel removes a client from a channel.
func (n *NVRD) LeaveChannel(id uint64, reason string) error {
	n.clientsMTX.Lock()
	defer n.clientsMTX.Unlock()
	n.channelsMTX.Lock()
	defer n.channelsMTX.Unlock()
	return n.leaveChannel(id, reason)
}

func (n *NVRD) leaveChannel(id uint64, reason string) error {
	c, ok := n.clients[id]
	if !ok {
		return errors.New("Client not found")
	}
	if c.ch == nil {
		return errors.New("Client not in a channel")
	}

	more, err := c.ch.leave(id, reason)
	if err != nil {
		return err
	}
	chEncrypted := c.ch.encrypted()
	if !more {
		delete(n.channels, c.ch.name)
		if !chEncrypted {
			n.numUnencryptedChannels--
		}
	}
	if !chEncrypted {
		n.numUnencryptedClients--
	}
	c.ch = nil

	return nil
}

// Send sends a message to the channel the client is a member of.
func (n *NVRD) Send(id uint64, msg ChannelMessage) error {
	n.clientsMTX.RLock()
	defer n.clientsMTX.RUnlock()
	n.channelsMTX.RLock()
	defer n.channelsMTX.RUnlock()

	c, ok := n.clients[id]
	if !ok {
		return errors.New("Client not found")
	}
	if c.ch == nil {
		return errors.New("Client not in a channel")
	}
	msg2 := make(ChannelMessage)
	for k, v := range msg {
		msg2[k] = v
	}
	msg2["origin"] = c.ID

	c.ch.broadcast(msg2, c.ID)
	return nil
}

// Kick kicks a client from NVRemoted.
func (n *NVRD) Kick(id uint64, reason string) error {
	n.clientsMTX.Lock()
	defer n.clientsMTX.Unlock()
	n.channelsMTX.Lock()
	defer n.channelsMTX.Unlock()

	c, ok := n.clients[id]
	if !ok {
		return errors.New("Client not found")
	}

	if c.ch != nil {
		if err := n.leaveChannel(id, reason); err != nil {
			n.log.WithFields(logrus.Fields{
				"nvremoted_client": c,
				"error":            err,
			}).Warn("Error removing client from channel")
		}
	}

	close(c.Send) // Tell client that NVRemoted went away.
	delete(n.clients, id)
	return nil
}

// Stats contains statistics about a running instance of NVRemoted.
type Stats struct {
	Uptime                 time.Duration `json:"uptime"`
	NumChannels            int           `json:"num_channels"`
	MaxChannels            int           `json:"max_channels"`
	MaxChannelsAt          time.Time     `json:"max_channels_at"`
	NumUnencryptedChannels int           `json:"num_unencrypted_channels"`
	NumClients             int           `json:"num_clients"`
	MaxClients             int           `json:"max_clients"`
	MaxClientsAt           time.Time     `json:"max_clients_at"`
	NumUnencryptedClients  int           `json:"num_unencrypted_clients"`
}

// Stats gets stats about the running instance of NVRemoted.
func (n *NVRD) Stats() Stats {
	n.clientsMTX.RLock()
	n.channelsMTX.RLock()
	defer n.clientsMTX.RUnlock()
	defer n.channelsMTX.RUnlock()

	return Stats{
		Uptime:                 time.Since(n.startedAt),
		NumChannels:            len(n.channels),
		MaxChannels:            n.maxChannels,
		MaxChannelsAt:          n.maxChannelsTime,
		NumUnencryptedChannels: n.numUnencryptedChannels,
		NumClients:             len(n.clients),
		MaxClients:             n.maxClients,
		MaxClientsAt:           n.maxClientsTime,
		NumUnencryptedClients:  n.numUnencryptedClients,
	}
}

// MOTDMessage contains the message of the day.
type MOTDMessage struct {
	model.DefaultMessage
	MOTD         string `json:"motd"`
	ForceDisplay bool   `json:"force_display"`
}

func newMOTDMessage(motd string, forceDisplay bool) MOTDMessage {
	return MOTDMessage{
		DefaultMessage: model.DefaultMessage{Type: "motd"},
		MOTD:           motd,
		ForceDisplay:   forceDisplay,
	}
}
