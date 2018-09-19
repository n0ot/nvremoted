package server

import (
	"log"

	"github.com/pkg/errors"

	"github.com/n0ot/nvremoted/pkg/models"
)

type Command struct {
	Client          *Client               // Originator
	Resp            chan<- models.Message // Replies go here
	Name            string                // Name of command
	Args            models.Message        // Arguments sent along with command
	ClientInitiated bool                  // If true, the command was sent by a client
}

// commandHandler handles a command sent to the server
type CommandHandler interface {
	Handle(*Server, *Command)
}

// CommandHandlerFunc is an adapter to use an ordinary function as a commandHandler.
type CommandHandlerFunc func(*Server, *Command)

// Handle calls f(command)
func (f CommandHandlerFunc) Handle(srv *Server, command *Command) {
	f(srv, command)
}

// InternalCommands contains commands available only for internal use.
var InternalCommands map[string]CommandHandler // Can be run by internal callers, but not by a client

// Commands contains commands available for clients to call.
var Commands map[string]CommandHandler // Client accessible commands

// Initialize command registries
func init() {
	InternalCommands = make(map[string]CommandHandler)
	InternalCommands["addclient"] = cmdAddclient
	InternalCommands["rmclient"] = cmdRmclient

	Commands = make(map[string]CommandHandler)
	Commands["protocol_version"] = cmdProtocolVersion
	Commands["join"] = cmdJoin
	Commands["pong"] = cmdPong
}

// Internal commands

// cmdAddclient adds a client to NVRemoted.
var cmdAddclient CommandHandlerFunc = func(srv *Server, command *Command) {
	if err := srv.AddClient(command.Client, command.Resp); err != nil {
		log.Printf("Cannot add %s to the server: %s", command.Client, err)
	}
}

// cmdRmclient removes a client from NVRemoted.
var cmdRmclient CommandHandlerFunc = func(srv *Server, command *Command) {
	reason := "Client disconnected"
	if v, ok := command.Args["reason"].(string); ok {
		reason = v
	}

	srv.Kick(command.Client.ID, reason)
}

// External commands

// cmdProtocolVersion sets the protocol version for a client.
// Currently, nothing is set, because only protocol version 2 is supported.
// If the protocol version is not 2, the client will be kicked.
var cmdProtocolVersion CommandHandlerFunc = func(srv *Server, command *Command) {
	if version, ok := command.Args["version"].(float64); !ok || version != 2.0 {
		command.Resp <- models.Message(map[string]interface{}{
			"type": "version_mismatch",
		})
		srv.Kick(command.Client.ID, "Version mismatch")
	}
}

// cmdJoin joins a channel
var cmdJoin CommandHandlerFunc = func(srv *Server, command *Command) {
	name, ok := command.Args["channel"].(string)
	if !ok || name == "" {
		command.Resp <- models.ErrorMessage("No channel name given")
		return
	}
	connectionType, ok := command.Args["connection_type"].(string)
	if !ok || connectionType == "" {
		command.Resp <- models.ErrorMessage("No connection_type given")
		return
	}

	if err := srv.JoinChannel(name, connectionType, command.Client.ID, command.Resp); err != nil {
		command.Resp <- models.ErrorMessage(err.Error())
	}
}

// cmdPong is a no-op.
// This command simply allows clients to reset their last seen timers.
var cmdPong CommandHandlerFunc = func(srv *Server, command *Command) {
}

// handleCommand looks up a command in the InternalCommands or Commands map
// and if found, runs it.
func (server *Server) handleCommand(command *Command) error {
	defer func() {
		if r := recover(); r != nil {
			errors.Errorf("Command: %s, %s: %s", command.Name, command.Client, r)
		}
	}()

	if command.Client == nil {
		return errors.Errorf("No client supplied in command")
	}

	resp := command.Resp
	if resp == nil {
		return errors.Errorf("Received command, but no response channel; command: %q", command)
	}

	if command.Name == "" {
		return errors.New("Command name is blank")
	}

	if _, ok := server.clients[command.Client.ID]; !ok && command.ClientInitiated {
		// If a client sends commands quickly, but is disconnected before all of them run,
		// the commands received here after the client was removed should be ignored.
		// This is not an error.
		return nil
	}

	var handler CommandHandler
	// If the command was run internally, it also has access to the internalCommands mapping
	if !command.ClientInitiated {
		handler = InternalCommands[command.Name]
	}

	// If a handler was found at this point,
	// don't override it with a client command.
	if handler == nil {
		handler = Commands[command.Name]
	}

	if handler == nil {
		// Relay all unknown commands to the channel for other clients to pick up.
		if server.clientActiveChannel[command.Client.ID] == nil {
			resp <- models.ErrorMessage("Type unknown, and not in a channel to relay")
			return nil
		}

		msg := models.Message(make(map[string]interface{}))
		for k, v := range command.Args {
			msg[k] = v
		}
		msg["origin"] = command.Client.ID
		server.clientActiveChannel[command.Client.ID].Broadcast(msg, command.Client.ID)
		return nil
	}

	handler.Handle(server, command)
	return nil
}

// parseCommand creates a Command from a Message.
func parseCommand(msg models.Message, client *Client, resp chan models.Message) (*Command, error) {
	commandName, ok := msg["type"].(string)
	if !ok {
		return nil, errors.New(`"type" key must be a string`)
	}

	return &Command{
		Client:          client,
		Resp:            resp,
		Name:            commandName,
		Args:            msg,
		ClientInitiated: true,
	}, nil
}
