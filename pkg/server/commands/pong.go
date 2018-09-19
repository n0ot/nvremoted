package commands

import "github.com/n0ot/nvremoted/pkg/server"

func init() {
	server.Commands["pong"] = cmdPong
}

// cmdPong is a no-op.
// This command simply allows clients to reset their last seen timers.
var cmdPong server.CommandHandlerFunc = func(srv *server.Server, command *server.Command) {
}
