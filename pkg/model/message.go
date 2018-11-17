package model

// A Message is sent to and from clients.
// All Messages should wrap DefaultMessage, so they have a Type field which marshals to json as "type."
// This interface allows generic messages to be passed.
type Message interface {
	Message() string
}

// DefaultMessage implements Message, and has a type.
type DefaultMessage struct {
	Type string `json:"type"`
}

// Message gets the type of a DefaultMessage.
// This ensures that DefaultMessage implements the Message interface.
func (msg DefaultMessage) Message() string {
	return msg.Type
}

// An ErrorMessage is sent to clients when an error has occured.
type ErrorMessage struct {
	DefaultMessage
	Error string `json:"error"`
}

// NewErrorMessage creates an error message with the specified reason.
func NewErrorMessage(reason string) ErrorMessage {
	return ErrorMessage{
		DefaultMessage: DefaultMessage{"error"},
		Error:          reason,
	}
}
