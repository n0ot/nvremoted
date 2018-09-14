package nvremoted

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
)

// Create a set of commands for the server
var internalCommands map[string]commandHandler // Can be run by internal callers, but not by a client
var commands map[string]commandHandler         // Client accessible commands

// commandHandler handles a command sent to the server
type commandHandler interface {
	Handle(*Server, *serverCommand)
}

// commandHandlerFunc is an adapter to use an ordinary function as a commandHandler.
// If f is a func(*server, *serverCommand), commandHandlerFunc(f)
// is a commandHandler whose Handle() method calls f.
type commandHandlerFunc func(*Server, *serverCommand)

// Handle calls f(command)
func (f commandHandlerFunc) Handle(server *Server, command *serverCommand) {
	f(server, command)
}

type serverCommand struct {
	client          *Client        // Originator
	responseChan    chan<- message // Replies go here
	command         string         // Name of command
	args            message        // Arguments sent along with command
	clientInitiated bool           // If true, the command was sent by a client
}

// Initialize the commands and internalCommands map,
// and add each command.
func init() {
	internalCommands = make(map[string]commandHandler)
	internalCommands["rmclient"] = cmdRmclient
	internalCommands["addclient"] = cmdAddclient

	commands = make(map[string]commandHandler)
	commands["join"] = cmdJoin
	commands["protocol_version"] = cmdProtocolVersion
	commands["pong"] = cmdPong
}

// Internal commands

// cmdRmclient removes a client from the server
var cmdRmclient commandHandlerFunc = func(server *Server, command *serverCommand) {
	reason := "Client disconnected"
	if v, ok := command.args["reason"].(string); ok {
		reason = v
	}

	server.kick(command.client.ID, reason)
}

// cmdAddclient adds a client to the server
var cmdAddclient commandHandlerFunc = func(server *Server, command *serverCommand) {
	if _, ok := server.clients[command.client.ID]; ok {
		log.Printf("%s already on the server; cannot add", command.client)
		return
	}

	server.clients[command.client.ID] = command.client
	server.clientResponseChan[command.client.ID] = command.responseChan
}

// Client commands

// cmdProtocolVersion sets the protocol version for a client.
// Currently, nothing is set, because only protocol version 2 is supported.
// If the protocol version is not 2, the client will be kicked.
var cmdProtocolVersion commandHandlerFunc = func(server *Server, command *serverCommand) {
	if version, ok := command.args["version"].(float64); !ok || version != 2.0 {
		command.responseChan <- message(map[string]interface{}{
			"type": "version_mismatch",
		})
		server.kick(command.client.ID, "Version mismatch")
	}
}

// cmdJoin joins a channel
var cmdJoin commandHandlerFunc = func(server *Server, command *serverCommand) {
	channelName, ok := command.args["channel"].(string)
	if !ok || channelName == "" {
		command.responseChan <- errorMessage("No channel name given")
		return
	}
	connectionType, ok := command.args["connection_type"].(string)
	if !ok || connectionType == "" {
		command.responseChan <- errorMessage("No connection_type given")
		return
	}

	newChannel, ok := server.channels[channelName]
	if !ok {
		newChannel = &channel{
			name:    channelName,
			members: make([]*channelMember, 0),
		}
		server.channels[channelName] = newChannel
	}

	for i := 0; i < len(newChannel.members); i++ {
		if command.client.ID == newChannel.members[i].ID {
			command.responseChan <- errorMessage("Already in channel")
			return
		}
	}

	oldChannelName, ok := server.clientActiveChannel[command.client.ID]
	if ok {
		err := leaveChannel(server, command.client.ID, oldChannelName, "Client switched channels")
		if err != nil {
			command.responseChan <- errorMessage("Cannot leave channel")
			return
		}
	}

	member := &channelMember{
		ID:             command.client.ID,
		ConnectionType: connectionType,
	}

	err := relayToChannel(server, channelName, message(map[string]interface{}{
		"type":   "client_joined",
		"client": member,
	}), nil)
	if err != nil {
		command.responseChan <- errorMessage("Cannot join channel")
		if len(newChannel.members) == 0 {
			delete(server.channels, newChannel.name)
		}
		return
	}

	command.responseChan <- message(map[string]interface{}{
		"origin":  command.client.ID,
		"clients": newChannel.members,
		"type":    "channel_joined",
		"channel": newChannel.name,
	})

	channelIsEncrypted := strings.HasPrefix(newChannel.name, "E2E_") && len(newChannel.name) == 68
	if server.config.WarnIfNotEncrypted && !channelIsEncrypted {
		command.responseChan <- message(map[string]interface{}{
			"type":          "motd",
			"motd":          "Your traffic will pass through this server unencrypted. Please consider upgrading to a version of NVDA Remote that supports end to end encryption.",
			"force_display": true,
		})
	}

	newChannel.members = append(newChannel.members, member)
	server.clientActiveChannel[command.client.ID] = newChannel.name
}

// cmdPong is a no-op.
// This command simply allows clients to reset their last seen timers.
var cmdPong commandHandlerFunc = func(server *Server, command *serverCommand) {
}

// Helper functions

// relayToChannel relays a message to all members of a channel.
// If excludeIDs is not nil, the IDs therein will not be included.
func relayToChannel(server *Server, channelName string, msg message, excludeIDs map[uint64]struct{}) error {
	channel, ok := server.channels[channelName]
	if !ok {
		return fmt.Errorf("No such channel")
	}

	for _, member := range channel.members {
		if excludeIDs != nil {
			if _, exists := excludeIDs[member.ID]; exists {
				continue
			}
		}

		responseChan := server.clientResponseChan[member.ID]
		if responseChan == nil {
			continue
		}
		responseChan <- msg
	}

	return nil
}

// leaveChannel leaves a channel
func leaveChannel(server *Server, id uint64, channelName, reason string) error {
	channel, ok := server.channels[channelName]
	if !ok {
		return fmt.Errorf("No such channel")
	}

	var member *channelMember
	for i := 0; i < len(channel.members); i++ {
		if id != channel.members[i].ID {
			continue
		}
		member = channel.members[i]
		channel.members = append(channel.members[:i], channel.members[i+1:]...)
		break
	}
	delete(server.clientActiveChannel, id)

	if member != nil {
		msg := message(map[string]interface{}{
			"type":   "client_left",
			"client": member,
		})
		if reason != "" {
			msg["reason"] = reason
		}

		relayToChannel(server, channelName, msg, nil)
	}

	if len(channel.members) == 0 {
		delete(server.channels, channelName)
	}

	return nil
}
