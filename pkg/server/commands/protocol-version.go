package commands

import (
	"github.com/n0ot/nvremoted/pkg/models"
	"github.com/n0ot/nvremoted/pkg/server"
)

func init() {
	server.Commands["protocol_version"] = cmdProtocolVersion
}

// cmdProtocolVersion sets the protocol version for a client.
// Currently, nothing is set, because only protocol version 2 is supported.
// If the protocol version is not 2, the client will be kicked.
var cmdProtocolVersion server.CommandHandlerFunc = func(srv *server.Server, command *server.Command) {
	if version, ok := command.Args["version"].(float64); !ok || version != 2.0 {
		command.Resp <- models.Message(map[string]interface{}{
			"type": "version_mismatch",
		})
		srv.Kick(command.Client.ID, "Version mismatch")
	}
}
