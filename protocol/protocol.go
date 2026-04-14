// Package protocol defines the interface for protocol implementations.
package protocol

import "net"

// Protocol defines the interface for a forwarding protocol.
type Protocol interface {
	// Name returns the protocol name.
	Name() string

	// Listen starts listening on the specified address.
	Listen(addr string) (net.Listener, error)

	// Dial connects to the specified address.
	Dial(addr string) (net.Conn, error)

	// Forward forwards data between two connections.
	Forward(src, dst net.Conn) error
}

// Config represents protocol configuration.
type Config struct {
	Name     string            `yaml:"name"`
	Listen   string            `yaml:"listen"`
	Target   string            `yaml:"target"`
	Options  map[string]string `yaml:"options"`
	Enabled  bool              `yaml:"enabled"`
}

// Registry manages registered protocols.
type Registry struct {
	protocols map[string]Protocol
}

// NewRegistry creates a new protocol registry.
func NewRegistry() *Registry {
	return &Registry{
		protocols: make(map[string]Protocol),
	}
}

// Register adds a protocol to the registry.
func (r *Registry) Register(p Protocol) {
	r.protocols[p.Name()] = p
}

// Get retrieves a protocol by name.
func (r *Registry) Get(name string) (Protocol, bool) {
	p, ok := r.protocols[name]
	return p, ok
}

// List returns all registered protocol names.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.protocols))
	for name := range r.protocols {
		names = append(names, name)
	}
	return names
}
