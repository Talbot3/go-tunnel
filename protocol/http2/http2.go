// Package http2 implements HTTP/2 protocol forwarding.
package http2

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sync"

	"golang.org/x/net/http2"

	"github.com/wld/go-tunnel/forward"
	"github.com/wld/go-tunnel/protocol"
)

const name = "http2"

// Protocol implements HTTP/2 forwarding.
type Protocol struct {
	forwarder forward.Forwarder
	tlsConfig *tls.Config
}

// New creates a new HTTP/2 protocol handler.
func New(tlsConfig *tls.Config) *Protocol {
	return &Protocol{
		forwarder: forward.NewForwarder(),
		tlsConfig: tlsConfig,
	}
}

// Name returns the protocol name.
func (p *Protocol) Name() string {
	return name
}

// Listen starts an HTTP/2 listener.
func (p *Protocol) Listen(addr string) (net.Listener, error) {
	return tls.Listen("tcp", addr, p.tlsConfig)
}

// Dial connects to an HTTP/2 server.
func (p *Protocol) Dial(addr string) (net.Conn, error) {
	return tls.Dial("tcp", addr, p.tlsConfig)
}

// Forward forwards data between two connections.
// For HTTP/2, we use the underlying TCP forwarding.
func (p *Protocol) Forward(src, dst net.Conn) error {
	return p.forwarder.Forward(src, dst)
}

// Client creates an HTTP/2 client.
func (p *Protocol) Client() *http.Client {
	transport := &http2.Transport{
		AllowHTTP: false,
		DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			return tls.Dial(network, addr, cfg)
		},
	}
	return &http.Client{Transport: transport}
}

// Server wraps an HTTP server with HTTP/2 support.
type Server struct {
	server *http.Server
	mu     sync.Mutex
}

// NewServer creates a new HTTP/2 server.
func NewServer(handler http.Handler, tlsConfig *tls.Config) *Server {
	s := &Server{
		server: &http.Server{
			Handler:   handler,
			TLSConfig: tlsConfig,
		},
	}
	http2.ConfigureServer(s.server, nil)
	return s
}

// Serve starts the server on the given listener.
func (s *Server) Serve(l net.Listener) error {
	return s.server.Serve(l)
}

// Close shuts down the server.
func (s *Server) Close() error {
	return s.server.Close()
}

// Config returns the default HTTP/2 protocol config.
func Config(listen, target string) protocol.Config {
	return protocol.Config{
		Name:    name,
		Listen:  listen,
		Target:  target,
		Enabled: true,
	}
}
