package commands

import (
	"log"

	"github.com/n0ot/nvremoted/pkg/server"
)

func init() {
	server.InternalCommands["addclient"] = cmdAddclient
}

// cmdAddclient adds a client to NVRemoted.
var cmdAddclient server.CommandHandlerFunc = func(srv *server.Server, command *server.Command) {
	if err := srv.AddClient(command.Client, command.Resp); err != nil {
		log.Printf("Cannot add %s to the server: %s", command.Client, err)
	}
}
