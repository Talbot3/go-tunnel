// Package quic implements QUIC protocol support for the tunnel library.
//
// This package provides QUIC protocol forwarding with built-in encryption
// using TLS 1.3. QUIC offers improved performance over TCP with reduced
// connection establishment latency and better congestion control.
//
// # Example
//
//	cfg := &tls.Config{
//	    Certificates: []tls.Certificate{cert},
//	    NextProtos:   []string{"quic-proxy"},
//	}
//	p := quic.New(cfg, nil)
//	listener, _ := p.Listen(":8443")
package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/Talbot3/go-tunnel/forward"
)

// Protocol implements QUIC protocol forwarding.
type Protocol struct {
	tlsConfig  *tls.Config
	quicConfig *quic.Config
	forwarder  forward.Forwarder
}

// New creates a new QUIC protocol handler.
// The tlsConfig must have NextProtos set for ALPN negotiation.
func New(tlsConfig *tls.Config, quicConfig *quic.Config) *Protocol {
	return &Protocol{
		tlsConfig:  tlsConfig,
		quicConfig: quicConfig,
		forwarder:  forward.NewForwarder(),
	}
}

// Name returns the protocol name.
func (p *Protocol) Name() string {
	return "quic"
}

// Listen creates a QUIC listener on the specified address.
// QUIC uses UDP for transport.
func (p *Protocol) Listen(addr string) (net.Listener, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}

	listener, err := quic.Listen(conn, p.tlsConfig, p.quicConfig)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return &quicListener{
		listener: listener,
	}, nil
}

// Dial connects to a QUIC server.
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
		tlsConf.NextProtos = []string{"quic-proxy"}
	}

	conn, err := quic.Dial(ctx, udpConn, udpAddr, tlsConf, p.quicConfig)
	if err != nil {
		udpConn.Close()
		return nil, err
	}

	return newQuicConn(conn), nil
}

// Forwarder returns the QUIC forwarder.
func (p *Protocol) Forwarder() forward.Forwarder {
	return p.forwarder
}

// quicListener wraps quic.Listener as net.Listener.
type quicListener struct {
	listener *quic.Listener
}

func (l *quicListener) Accept() (net.Conn, error) {
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

	return &quicStreamConn{
		Stream: stream,
		conn:   conn,
	}, nil
}

func (l *quicListener) Close() error {
	return l.listener.Close()
}

func (l *quicListener) Addr() net.Addr {
	return l.listener.Addr()
}

// quicConn wraps quic.Connection as net.Conn with persistent stream management.
type quicConn struct {
	conn        quic.Connection
	stream      quic.Stream
	streamMutex sync.Mutex
	closed      bool
	closeMutex  sync.Mutex
}

// newQuicConn creates a new quicConn and opens a persistent stream.
func newQuicConn(conn quic.Connection) *quicConn {
	c := &quicConn{conn: conn}
	// Open a stream lazily on first use
	return c
}

// getOrCreateStream returns the existing stream or creates a new one.
func (c *quicConn) getOrCreateStream() (quic.Stream, error) {
	c.streamMutex.Lock()
	defer c.streamMutex.Unlock()

	if c.stream != nil {
		return c.stream, nil
	}

	stream, err := c.conn.OpenStreamSync(context.Background())
	if err != nil {
		return nil, err
	}
	c.stream = stream
	return stream, nil
}

func (c *quicConn) Read(b []byte) (n int, err error) {
	stream, err := c.getOrCreateStream()
	if err != nil {
		return 0, err
	}
	return stream.Read(b)
}

func (c *quicConn) Write(b []byte) (n int, err error) {
	stream, err := c.getOrCreateStream()
	if err != nil {
		return 0, err
	}
	return stream.Write(b)
}

func (c *quicConn) Close() error {
	c.closeMutex.Lock()
	defer c.closeMutex.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.stream != nil {
		c.stream.Close()
	}
	return c.conn.CloseWithError(0, "connection closed")
}

func (c *quicConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *quicConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *quicConn) SetDeadline(t time.Time) error {
	if c.stream != nil {
		return c.stream.SetDeadline(t)
	}
	return nil
}

func (c *quicConn) SetReadDeadline(t time.Time) error {
	if c.stream != nil {
		return c.stream.SetReadDeadline(t)
	}
	return nil
}

func (c *quicConn) SetWriteDeadline(t time.Time) error {
	if c.stream != nil {
		return c.stream.SetWriteDeadline(t)
	}
	return nil
}

// quicStreamConn wraps a QUIC stream as a net.Conn.
type quicStreamConn struct {
	quic.Stream
	conn   quic.Connection
	closed bool
	mu     sync.Mutex
}

func (c *quicStreamConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	return c.Stream.Close()
}

func (c *quicStreamConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *quicStreamConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// DefaultConfig returns the default QUIC configuration.
func DefaultConfig() *quic.Config {
	return &quic.Config{
		MaxIncomingStreams:    1000,
		MaxIncomingUniStreams: 1000,
		KeepAlivePeriod:       30 * time.Second,
	}
}

// IsQUICClosedErr returns true if the error indicates a normal QUIC connection close.
func IsQUICClosedErr(err error) bool {
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
