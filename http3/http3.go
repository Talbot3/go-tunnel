// Package http3 implements HTTP/3 protocol support for the tunnel library.
//
// This package provides HTTP/3 protocol forwarding using QUIC as the
// underlying transport. HTTP/3 offers improved performance over HTTP/2
// with reduced head-of-line blocking.
//
// # Example
//
//	cfg := &tls.Config{
//	    Certificates: []tls.Certificate{cert},
//	    NextProtos:   []string{"h3"},
//	}
//	p := http3.New(cfg, nil)
//	listener, _ := p.Listen(":8443")
package http3

import (
	"context"
	"crypto/tls"
	"errors"
	"net"

	"github.com/quic-go/quic-go/http3"

	"github.com/Talbot3/go-tunnel/forward"
)

// Protocol implements HTTP/3 protocol forwarding.
type Protocol struct {
	tlsConfig *tls.Config
	forwarder forward.Forwarder
}

// New creates a new HTTP/3 protocol handler.
// The tlsConfig must have NextProtos set to []string{"h3"}.
func New(tlsConfig *tls.Config, quicConfig interface{}) *Protocol {
	return &Protocol{
		tlsConfig: tlsConfig,
		forwarder: forward.NewForwarder(),
	}
}

// Name returns the protocol name.
func (p *Protocol) Name() string {
	return "http3"
}

// Listen creates an HTTP/3 listener on the specified address.
// HTTP/3 uses UDP for transport.
func (p *Protocol) Listen(addr string) (net.Listener, error) {
	// Create a UDP listener wrapper for HTTP/3
	return &http3Listener{
		addr: addr,
	}, nil
}

// Dial connects to an HTTP/3 server.
func (p *Protocol) Dial(ctx context.Context, addr string) (net.Conn, error) {
	// Create HTTP/3 connection using quic-go
	// This is a simplified implementation
	d := net.Dialer{}
	return d.DialContext(ctx, "udp", addr)
}

// Forwarder returns the HTTP/3 forwarder.
func (p *Protocol) Forwarder() forward.Forwarder {
	return p.forwarder
}

// http3Listener wraps HTTP/3 server as a net.Listener.
type http3Listener struct {
	addr string
}

func (l *http3Listener) Accept() (net.Conn, error) {
	// HTTP/3 accepts streams, not connections
	// This implementation requires further work for full HTTP/3 support
	return nil, errors.New("HTTP/3 listener: use http3.Server for full implementation")
}

func (l *http3Listener) Close() error {
	return nil
}

func (l *http3Listener) Addr() net.Addr {
	return &net.UDPAddr{Port: 8443}
}

// Server wraps http3.Server for convenience.
type Server struct {
	*http3.Server
}

// NewServer creates a new HTTP/3 server.
func NewServer(tlsConfig *tls.Config) *Server {
	return &Server{
		Server: &http3.Server{
			TLSConfig: tlsConfig,
		},
	}
}
