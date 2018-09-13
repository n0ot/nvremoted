package nvremoted

import "errors"

// message is passed through the server, to be sent to clients.
type message map[string]interface{}

var kickMessage = message(map[string]interface{}{
	"type": "kick",
})

// errorMessage creates a message of type error and the given reason.
func errorMessage(reason string) message {
	return message(map[string]interface{}{
		"type":   "error",
		"reason": reason,
	})
}

// parseCommand creates a server command from a message.
func parseCommand(msg message, client *Client, responseChan chan message) (*serverCommand, error) {
	commandName, ok := msg["type"].(string)
	if !ok {
		return nil, errors.New(`"type" key must be a string`)
	}

	return &serverCommand{
		client:          client,
		responseChan:    responseChan,
		command:         commandName,
		args:            msg,
		clientInitiated: true,
	}, nil
}
