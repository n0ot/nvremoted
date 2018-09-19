package models

// Message is passed through the server, to be sent to clients.
type Message map[string]interface{}

// NewMessage creates a new empty message.
func NewMessage() Message {
	return make(map[string]interface{})
}

// ErrorMessage creates a message of type error and the given reason.
func ErrorMessage(reason string) Message {
	return Message(map[string]interface{}{
		"type":   "error",
		"reason": reason,
	})
}
