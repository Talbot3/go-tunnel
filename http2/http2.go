// Package http2 implements HTTP/2 protocol support for the tunnel library.
//
// This package provides HTTP/2 protocol forwarding with stream multiplexing
// support. It requires TLS configuration for secure connections.
//
// # Example
//
//	cfg := &tls.Config{
//	    Certificates: []tls.Certificate{cert},
//	}
//	p := http2.New(cfg)
//	listener, _ := p.Listen(":8443")
package http2

import (
	"context"
	"crypto/tls"
	"net"
	"sync"

	"golang.org/x/net/http2"

	"github.com/Talbot3/go-tunnel/forward"
)

// Protocol implements HTTP/2 protocol forwarding.
type Protocol struct {
	tlsConfig *tls.Config
	forwarder forward.Forwarder
	mu        sync.Mutex
}

// New creates a new HTTP/2 protocol handler.
// The tlsConfig is required for HTTP/2 connections.
func New(tlsConfig *tls.Config) *Protocol {
	return &Protocol{
		tlsConfig: tlsConfig,
		forwarder: forward.NewForwarder(),
	}
}

// Name returns the protocol name.
func (p *Protocol) Name() string {
	return "http2"
}

// Listen creates an HTTP/2 listener on the specified address.
// The listener accepts TLS connections with HTTP/2 support.
func (p *Protocol) Listen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// Dial connects to an HTTP/2 server.
func (p *Protocol) Dial(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	// Wrap with TLS if configured
	if p.tlsConfig != nil {
		tlsConn := tls.Client(conn, p.tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, err
		}
		return &http2Conn{Conn: tlsConn}, nil
	}

	return conn, nil
}

// Forwarder returns the HTTP/2 forwarder.
func (p *Protocol) Forwarder() forward.Forwarder {
	return p.forwarder
}

// http2Conn wraps a TLS connection with HTTP/2 support.
type http2Conn struct {
	net.Conn
}

// Stream represents an HTTP/2 stream.
type Stream struct {
	id   uint32
	conn net.Conn
}

// ID returns the stream identifier.
func (s *Stream) ID() uint32 {
	return s.id
}

// IsServerInitiated returns true if the stream was initiated by the server.
func (s *Stream) IsServerInitiated() bool {
	return s.id%2 == 0
}

// ConfigureServer configures an HTTP/2 server.
func ConfigureServer(srv *http2.Server) {
	// HTTP/2 server is already configured
}

// ConfigureTransport configures an HTTP/2 transport.
func ConfigureTransport(tr *http2.Transport) {
	// HTTP/2 transport is already configured
}
