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
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"github.com/Talbot3/go-tunnel/forward"
)

// Protocol implements HTTP/3 protocol forwarding.
type Protocol struct {
	tlsConfig  *tls.Config
	quicConfig *quic.Config
	forwarder  forward.Forwarder
}

// New creates a new HTTP/3 protocol handler.
// The tlsConfig must have NextProtos set to []string{"h3"}.
func New(tlsConfig *tls.Config, quicConfig *quic.Config) *Protocol {
	return &Protocol{
		tlsConfig:  tlsConfig,
		quicConfig: quicConfig,
		forwarder:  forward.NewForwarder(),
	}
}

// Name returns the protocol name.
func (p *Protocol) Name() string {
	return "http3"
}

// Listen creates an HTTP/3 listener on the specified address.
// HTTP/3 uses UDP for transport.
func (p *Protocol) Listen(addr string) (net.Listener, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}

	// Create QUIC listener
	quicListener, err := quic.Listen(udpConn, p.tlsConfig, p.quicConfig)
	if err != nil {
		udpConn.Close()
		return nil, err
	}

	return &http3Listener{
		listener: quicListener,
		udpConn:  udpConn,
	}, nil
}

// Dial connects to an HTTP/3 server.
func (p *Protocol) Dial(ctx context.Context, addr string) (net.Conn, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}

	tlsConf := p.tlsConfig.Clone()
	if len(tlsConf.NextProtos) == 0 {
		tlsConf.NextProtos = []string{"h3"}
	}

	quicConn, err := quic.Dial(ctx, udpConn, udpAddr, tlsConf, p.quicConfig)
	if err != nil {
		udpConn.Close()
		return nil, err
	}

	// Open a stream for the connection
	stream, err := quicConn.OpenStreamSync(ctx)
	if err != nil {
		quicConn.CloseWithError(1, "failed to open stream")
		return nil, err
	}

	return &http3Conn{
		conn:   quicConn,
		stream: stream,
	}, nil
}

// Forwarder returns the HTTP/3 forwarder.
func (p *Protocol) Forwarder() forward.Forwarder {
	return p.forwarder
}

// http3Listener wraps QUIC listener as net.Listener.
type http3Listener struct {
	listener *quic.Listener
	udpConn  *net.UDPConn
	mu       sync.Mutex
	closed   bool
}

// Accept accepts a new QUIC connection and returns the first stream as a net.Conn.
func (l *http3Listener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, net.ErrClosed
	}
	l.mu.Unlock()

	conn, err := l.listener.Accept(context.Background())
	if err != nil {
		return nil, err
	}

	// Accept the first stream as the main connection
	stream, err := conn.AcceptStream(context.Background())
	if err != nil {
		conn.CloseWithError(1, "failed to accept stream")
		return nil, err
	}

	return &http3StreamConn{
		stream: stream,
		conn:   conn,
	}, nil
}

// Close closes the listener.
func (l *http3Listener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}
	l.closed = true

	err := l.listener.Close()
	if l.udpConn != nil {
		l.udpConn.Close()
	}
	return err
}

// Addr returns the listener address.
func (l *http3Listener) Addr() net.Addr {
	if l.listener != nil {
		return l.listener.Addr()
	}
	return nil
}

// http3Conn wraps a QUIC connection with a stream for HTTP/3 client.
type http3Conn struct {
	conn   quic.Connection
	stream quic.Stream
	mu     sync.Mutex
	closed bool
}

func (c *http3Conn) Read(b []byte) (n int, err error) {
	return c.stream.Read(b)
}

func (c *http3Conn) Write(b []byte) (n int, err error) {
	return c.stream.Write(b)
}

func (c *http3Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	c.stream.Close()
	return c.conn.CloseWithError(0, "connection closed")
}

func (c *http3Conn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *http3Conn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *http3Conn) SetDeadline(t time.Time) error {
	return c.stream.SetDeadline(t)
}

func (c *http3Conn) SetReadDeadline(t time.Time) error {
	return c.stream.SetReadDeadline(t)
}

func (c *http3Conn) SetWriteDeadline(t time.Time) error {
	return c.stream.SetWriteDeadline(t)
}

// http3StreamConn wraps a QUIC stream as a net.Conn for server-side connections.
type http3StreamConn struct {
	stream quic.Stream
	conn   quic.Connection
	mu     sync.Mutex
	closed bool
}

func (c *http3StreamConn) Read(b []byte) (n int, err error) {
	return c.stream.Read(b)
}

func (c *http3StreamConn) Write(b []byte) (n int, err error) {
	return c.stream.Write(b)
}

func (c *http3StreamConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	// Close the stream, but keep the QUIC connection for potential reuse
	return c.stream.Close()
}

func (c *http3StreamConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *http3StreamConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *http3StreamConn) SetDeadline(t time.Time) error {
	return c.stream.SetDeadline(t)
}

func (c *http3StreamConn) SetReadDeadline(t time.Time) error {
	return c.stream.SetReadDeadline(t)
}

func (c *http3StreamConn) SetWriteDeadline(t time.Time) error {
	return c.stream.SetWriteDeadline(t)
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

// DefaultConfig returns the default QUIC configuration for HTTP/3.
func DefaultConfig() *quic.Config {
	return &quic.Config{
		MaxIncomingStreams:     1000,
		MaxIncomingUniStreams:  1000,
		KeepAlivePeriod:        30 * time.Second,
		MaxIdleTimeout:         30 * time.Second,
		HandshakeIdleTimeout:   10 * time.Second,
	}
}

// IsHTTP3ClosedErr returns true if the error indicates a normal HTTP/3 connection close.
func IsHTTP3ClosedErr(err error) bool {
	if err == nil {
		return true
	}

	// Check for QUIC application error
	var appErr *quic.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.ErrorCode == 0
	}

	// Check for QUIC transport error
	var transportErr *quic.TransportError
	if errors.As(err, &transportErr) {
		return false // Transport errors are not normal closes
	}

	return false
}
