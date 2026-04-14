// Package quic implements QUIC protocol forwarding.
package quic

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/wld/go-tunnel/forward"
	"github.com/wld/go-tunnel/protocol"
)

const name = "quic"

// Protocol implements QUIC forwarding.
type Protocol struct {
	forwarder  forward.Forwarder
	tlsConfig  *tls.Config
	quicConfig *quic.Config
}

// New creates a new QUIC protocol handler.
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

// Listen creates a QUIC listener.
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
	return &quicListener{listener: *listener, conn: conn}, nil
}

// Dial connects to a QUIC server.
func (p *Protocol) Dial(addr string) (net.Conn, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	udpConn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return nil, err
	}

	session, err := quic.Dial(context.Background(), udpConn, udpAddr, p.tlsConfig, p.quicConfig)
	if err != nil {
		udpConn.Close()
		return nil, err
	}

	stream, err := session.OpenStreamSync(context.Background())
	if err != nil {
		session.CloseWithError(0, "")
		udpConn.Close()
		return nil, err
	}

	return &quicStreamConn{
		stream:  stream,
		session: session,
		conn:    udpConn,
	}, nil
}

// Forward forwards data between two connections.
func (p *Protocol) Forward(src, dst net.Conn) error {
	return p.forwarder.Forward(src, dst)
}

// quicListener wraps a QUIC listener as a net.Listener.
type quicListener struct {
	listener quic.Listener
	conn     *net.UDPConn
}

// Accept accepts a new QUIC connection.
func (l *quicListener) Accept() (net.Conn, error) {
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

// Close closes the listener.
func (l *quicListener) Close() error {
	err := l.listener.Close()
	l.conn.Close()
	return err
}

// Addr returns the listener address.
func (l *quicListener) Addr() net.Addr {
	return l.listener.Addr()
}

// quicStreamConn wraps a QUIC stream as a net.Conn.
type quicStreamConn struct {
	stream  quic.Stream
	session quic.Connection
	conn    *net.UDPConn
}

func (c *quicStreamConn) Read(b []byte) (n int, err error)  { return c.stream.Read(b) }
func (c *quicStreamConn) Write(b []byte) (n int, err error) { return c.stream.Write(b) }
func (c *quicStreamConn) Close() error {
	c.stream.Close()
	return nil
}
func (c *quicStreamConn) LocalAddr() net.Addr {
	if c.conn != nil {
		return c.conn.LocalAddr()
	}
	return c.session.LocalAddr()
}
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

// Config returns the default QUIC protocol config.
func Config(listen, target string) protocol.Config {
	return protocol.Config{
		Name:    name,
		Target:  target,
		Listen:  listen,
		Enabled: true,
	}
}
