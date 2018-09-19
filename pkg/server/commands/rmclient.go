package commands

import "github.com/n0ot/nvremoted/pkg/server"

func init() {
	server.InternalCommands["rmclient"] = cmdRmclient
}

// cmdRmclient removes a client from NVRemoted.
var cmdRmclient server.CommandHandlerFunc = func(srv *server.Server, command *server.Command) {
	reason := "Client disconnected"
	if v, ok := command.Args["reason"].(string); ok {
		reason = v
	}

	srv.Kick(command.Client.ID, reason)
}
