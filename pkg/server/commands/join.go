package commands

import (
	"github.com/n0ot/nvremoted/pkg/models"
	"github.com/n0ot/nvremoted/pkg/server"
)

func init() {
	server.Commands["join"] = cmdJoin
}

// cmdJoin joins a channel
var cmdJoin server.CommandHandlerFunc = func(srv *server.Server, command *server.Command) {
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
