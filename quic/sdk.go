// Package quic provides SDK utilities for QUIC tunnel connections.
package quic

import (
	"context"
	"encoding/binary"
	"net"
	"time"
)

// DefaultDialTimeout is the default timeout for tunnel connections.
const DefaultDialTimeout = 10 * time.Second

// DialTunnel connects to a tunnel server with domain-based routing.
// This is a convenience function that handles the first-frame domain protocol.
//
// Format: [2B DomainLen][Domain][Payload...]
//
// Example:
//
//	conn, err := DialTunnel("app1.tunnel.com", "tunnel.example.com:443")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer conn.Close()
//	conn.Write([]byte("Hello!"))
func DialTunnel(domain, addr string) (net.Conn, error) {
	return DialTunnelContext(context.Background(), domain, addr)
}

// DialTunnelContext connects to a tunnel server with domain-based routing using context.
func DialTunnelContext(ctx context.Context, domain, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: DefaultDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	// Send first frame with domain
	domainBytes := []byte(domain)
	header := make([]byte, 2+len(domainBytes))
	binary.BigEndian.PutUint16(header[0:2], uint16(len(domainBytes)))
	copy(header[2:], domainBytes)

	if _, err := conn.Write(header); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

// DialTunnelWithPayload connects to a tunnel server and sends initial payload.
// This combines DialTunnel with sending initial data in one operation.
//
// Example:
//
//	conn, err := DialTunnelWithPayload("app1.tunnel.com", "tunnel.example.com:443", []byte("GET / HTTP/1.1\r\nHost: app1.tunnel.com\r\n\r\n"))
func DialTunnelWithPayload(domain, addr string, payload []byte) (net.Conn, error) {
	return DialTunnelWithPayloadContext(context.Background(), domain, addr, payload)
}

// DialTunnelWithPayloadContext connects to a tunnel server and sends initial payload using context.
func DialTunnelWithPayloadContext(ctx context.Context, domain, addr string, payload []byte) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: DefaultDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	// Send first frame with domain and payload
	domainBytes := []byte(domain)
	header := make([]byte, 2+len(domainBytes)+len(payload))
	binary.BigEndian.PutUint16(header[0:2], uint16(len(domainBytes)))
	copy(header[2:], domainBytes)
	copy(header[2+len(domainBytes):], payload)

	if _, err := conn.Write(header); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

// TunnelConn wraps a net.Conn with tunnel domain information.
type TunnelConn struct {
	net.Conn
	Domain string
}

// NewTunnelConn creates a new TunnelConn with the given domain.
func NewTunnelConn(conn net.Conn, domain string) *TunnelConn {
	return &TunnelConn{
		Conn:   conn,
		Domain: domain,
	}
}

// DialTunnelConn creates a new TunnelConn by connecting to the tunnel server.
func DialTunnelConn(domain, addr string) (*TunnelConn, error) {
	return DialTunnelConnContext(context.Background(), domain, addr)
}

// DialTunnelConnContext creates a new TunnelConn using context.
func DialTunnelConnContext(ctx context.Context, domain, addr string) (*TunnelConn, error) {
	conn, err := DialTunnelContext(ctx, domain, addr)
	if err != nil {
		return nil, err
	}
	return NewTunnelConn(conn, domain), nil
}
