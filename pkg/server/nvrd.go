package server

import (
	"log"
	"strings"

	"github.com/pkg/errors"

	"github.com/n0ot/nvremoted/pkg/channels"
	"github.com/n0ot/nvremoted/pkg/models"
)

// AddClient adds a client to NVRemoted.
func (srv *Server) AddClient(client *Client, resp chan<- models.Message) error {
	if _, ok := srv.clients[client.ID]; ok {
		return errors.New("Client ID already exists")
	}

	srv.clients[client.ID] = client
	srv.clientResp[client.ID] = resp

	if srv.config.Motd != "" {
		resp <- models.Message{
			"type": "motd",
			"motd": srv.config.Motd,
		}
	}
	return nil
}

// JoinChannel adds a client to a channel.
// If the client is in another channel,
// they will be removed from the other channel first.
func (srv *Server) JoinChannel(name, connectionType string, id uint64, resp chan<- models.Message) error {
	ch, existingCh := srv.channels[name]
	if !existingCh {
		ch = channels.New(name)
	}

	if oldCh, ok := srv.clientActiveChannel[id]; ok {
		if err := srv.LeaveChannel(oldCh, id, "Client switched channels"); err != nil {
			return err
		}
	}

	if err := ch.Join(id, connectionType, resp); err != nil {
		return err
	}
	if !existingCh {
		srv.channels[name] = ch
	}

	channelIsEncrypted := strings.HasPrefix(ch.Name, "E2E_") && len(ch.Name) == 68
	if srv.config.WarnIfNotEncrypted && !channelIsEncrypted {
		resp <- models.Message{
			"type":          "motd",
			"motd":          "Your traffic will pass through this server unencrypted. Please consider upgrading to a version of NVDA Remote that supports end to end encryption.",
			"force_display": true,
		}
	}

	srv.clientActiveChannel[id] = ch
	return nil
}

// LeaveChannel removes a client from a channel.
func (srv *Server) LeaveChannel(ch *channels.Channel, id uint64, reason string) error {
	more, err := ch.Leave(id, reason)
	if err != nil {
		return errors.New("Cannot leave channel")
	}
	delete(srv.clientActiveChannel, id)
	if !more {
		delete(srv.channels, ch.Name)
	}

	return nil
}

// Kick kicks a client from NVRemoted.
func (srv *Server) Kick(clientID uint64, reason string) error {
	if _, ok := srv.clients[clientID]; !ok {
		errors.Errorf("Client(%s) not found; cannot kick", clientID)
	}

	if ch := srv.clientActiveChannel[clientID]; ch != nil {
		if err := srv.LeaveChannel(ch, clientID, reason); err != nil {
			log.Printf("Error removing client %d from channel %s: %s", clientID, ch.Name, err)
		}
	}

	close(srv.clientResp[clientID]) // Signals client handler to kick client.
	delete(srv.clientResp, clientID)
	delete(srv.clients, clientID)
	return nil
}
