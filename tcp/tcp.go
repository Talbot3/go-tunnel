// Package tcp implements TCP protocol support for the tunnel library.
package tcp

import (
	"context"
	"net"

	"github.com/Talbot3/go-tunnel/forward"
)

// Protocol implements TCP protocol forwarding.
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
	return "tcp"
}

// Listen creates a TCP listener on the specified address.
func (p *Protocol) Listen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// Dial connects to a TCP address.
func (p *Protocol) Dial(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

// Forwarder returns the TCP forwarder.
func (p *Protocol) Forwarder() forward.Forwarder {
	return p.forwarder
}
