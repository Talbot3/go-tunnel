// Package http3 implements HTTP/3 protocol forwarding.
package http3

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"github.com/wld/go-tunnel/forward"
	"github.com/wld/go-tunnel/protocol"
)

const name = "http3"

// Protocol implements HTTP/3 forwarding.
type Protocol struct {
	forwarder  forward.Forwarder
	tlsConfig  *tls.Config
	quicConfig *quic.Config
}

// New creates a new HTTP/3 protocol handler.
func New(tlsConfig *tls.Config, quicConfig *quic.Config) *Protocol {
	if quicConfig == nil {
		quicConfig = &quic.Config{
			MaxIncomingStreams: 1000,
		}
	}
	return &Protocol{
		forwarder:  forward.NewForwarder(),
		tlsConfig:  tlsConfig,
		quicConfig: quicConfig,
	}
}

// Name returns the protocol name.
func (p *Protocol) Name() string {
	return name
}

// Listen creates an HTTP/3 listener.
func (p *Protocol) Listen(addr string) (net.Listener, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}

	// Create HTTP/3 server
	server := &http3.Server{
		TLSConfig:  p.tlsConfig,
		QuicConfig: p.quicConfig,
	}

	// Start listening
	listener, err := quic.Listen(udpConn, p.tlsConfig, p.quicConfig)
	if err != nil {
		udpConn.Close()
		return nil, err
	}

	return &http3Listener{server: server, listener: *listener, conn: udpConn}, nil
}

// Dial connects to an HTTP/3 server.
func (p *Protocol) Dial(addr string) (net.Conn, error) {
	roundTripper := &http3.RoundTripper{
		TLSClientConfig: p.tlsConfig,
		QuicConfig:      p.quicConfig,
	}

	// For HTTP/3, we need to establish a connection first
	// This is a simplified implementation
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, err
	}

	return &http3Conn{conn: conn, roundTripper: roundTripper}, nil
}

// Forward forwards data between two connections.
func (p *Protocol) Forward(src, dst net.Conn) error {
	return p.forwarder.Forward(src, dst)
}

// http3Listener wraps an HTTP/3 listener.
type http3Listener struct {
	server   *http3.Server
	listener quic.Listener
	conn     *net.UDPConn
}

func (l *http3Listener) Accept() (net.Conn, error) {
	session, err := l.listener.Accept(context.Background())
	if err != nil {
		return nil, err
	}
	stream, err := session.AcceptStream(context.Background())
	if err != nil {
		session.CloseWithError(0, "")
		return nil, err
	}
	return &quicStreamConn{stream: stream, session: session}, nil
}

func (l *http3Listener) Close() error {
	err := l.listener.Close()
	l.conn.Close()
	return err
}

func (l *http3Listener) Addr() net.Addr {
	return l.listener.Addr()
}

// quicStreamConn wraps a QUIC stream as a net.Conn.
type quicStreamConn struct {
	stream  quic.Stream
	session quic.Connection
}

func (c *quicStreamConn) Read(b []byte) (n int, err error)  { return c.stream.Read(b) }
func (c *quicStreamConn) Write(b []byte) (n int, err error) { return c.stream.Write(b) }
func (c *quicStreamConn) Close() error {
	c.stream.Close()
	return nil
}
func (c *quicStreamConn) LocalAddr() net.Addr  { return c.session.LocalAddr() }
func (c *quicStreamConn) RemoteAddr() net.Addr { return c.session.RemoteAddr() }
func (c *quicStreamConn) SetDeadline(t time.Time) error {
	return c.stream.SetDeadline(t)
}
func (c *quicStreamConn) SetReadDeadline(t time.Time) error {
	return c.stream.SetReadDeadline(t)
}
func (c *quicStreamConn) SetWriteDeadline(t time.Time) error {
	return c.stream.SetWriteDeadline(t)
}

// http3Conn wraps an HTTP/3 connection.
type http3Conn struct {
	conn         net.Conn
	roundTripper *http3.RoundTripper
}

func (c *http3Conn) Read(b []byte) (n int, err error)  { return c.conn.Read(b) }
func (c *http3Conn) Write(b []byte) (n int, err error) { return c.conn.Write(b) }
func (c *http3Conn) Close() error {
	c.roundTripper.Close()
	return c.conn.Close()
}
func (c *http3Conn) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *http3Conn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }
func (c *http3Conn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}
func (c *http3Conn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}
func (c *http3Conn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

// Server creates an HTTP/3 server.
func (p *Protocol) Server(handler http.Handler) *http3.Server {
	return &http3.Server{
		Handler:    handler,
		TLSConfig:  p.tlsConfig,
		QuicConfig: p.quicConfig,
	}
}

// Client creates an HTTP/3 client.
func (p *Protocol) Client() *http.Client {
	return &http.Client{
		Transport: &http3.RoundTripper{
			TLSClientConfig: p.tlsConfig,
			QuicConfig:      p.quicConfig,
		},
	}
}

// Config returns the default HTTP/3 protocol config.
func Config(listen, target string) protocol.Config {
	return protocol.Config{
		Name:    name,
		Listen:  listen,
		Target:  target,
		Enabled: true,
	}
}
