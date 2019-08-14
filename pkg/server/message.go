package server

// A Message encapsulates events passed through the server.
type Message interface {
	Name() string
}
