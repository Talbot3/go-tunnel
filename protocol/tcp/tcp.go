// Package tcp implements TCP protocol forwarding.
package tcp

import (
	"net"

	"github.com/wld/go-tunnel/forward"
	"github.com/wld/go-tunnel/protocol"
)

const name = "tcp"

// Protocol implements TCP forwarding.
type Protocol struct {
	forwarder forward.Forwarder
}

// New creates a new TCP protocol handler.
func New() *Protocol {
	return &Protocol{
		forwarder: forward.NewForwarder(),
	}
}

// Name returns the protocol name.
func (p *Protocol) Name() string {
	return name
}

// Listen starts a TCP listener.
func (p *Protocol) Listen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// Dial connects to a TCP address.
func (p *Protocol) Dial(addr string) (net.Conn, error) {
	return net.Dial("tcp", addr)
}

// Forward forwards data between two TCP connections.
func (p *Protocol) Forward(src, dst net.Conn) error {
	return p.forwarder.Forward(src, dst)
}

// Config returns the default TCP protocol config.
func Config(listen, target string) protocol.Config {
	return protocol.Config{
		Name:    name,
		Listen:  listen,
		Target:  target,
		Enabled: true,
	}
}
