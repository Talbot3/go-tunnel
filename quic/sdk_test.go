package quic_test

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	tunnelquic "github.com/Talbot3/go-tunnel/quic"
)

// TestDialTunnel tests the DialTunnel SDK function
func TestDialTunnel(t *testing.T) {
	// Start a server that simulates tunnel behavior - reads domain header, then echoes data
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	defer listener.Close()

	echoAddr := listener.Addr().String()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Read domain header
				var domainLenBuf [2]byte
				if _, err := io.ReadFull(c, domainLenBuf[:]); err != nil {
					return
				}
				domainLen := binary.BigEndian.Uint16(domainLenBuf[:])
				domain := make([]byte, domainLen)
				if _, err := io.ReadFull(c, domain); err != nil {
					return
				}
				// Now echo remaining data
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Test DialTunnel
	conn, err := tunnelquic.DialTunnel("test.example.com", echoAddr)
	if err != nil {
		t.Fatalf("DialTunnel failed: %v", err)
	}
	defer conn.Close()

	// Verify connection works
	testData := []byte("Hello, Tunnel!")
	if _, err := conn.Write(testData); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	response := make([]byte, len(testData))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, response); err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if string(response) != string(testData) {
		t.Errorf("Expected %q, got %q", string(testData), string(response))
	}

	t.Logf("DialTunnel test passed")
}

// TestDialTunnelWithPayload tests the DialTunnelWithPayload SDK function
func TestDialTunnelWithPayload(t *testing.T) {
	// Start a server that reads domain header and payload, then echoes
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Read domain header
				var domainLenBuf [2]byte
				if _, err := io.ReadFull(c, domainLenBuf[:]); err != nil {
					return
				}
				domainLen := binary.BigEndian.Uint16(domainLenBuf[:])
				domain := make([]byte, domainLen)
				if _, err := io.ReadFull(c, domain); err != nil {
					return
				}
				// Echo remaining data (payload)
				io.Copy(c, c)
			}(conn)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Test DialTunnelWithPayload
	domain := "app.example.com"
	payload := []byte("Initial Payload")
	conn, err := tunnelquic.DialTunnelWithPayload(domain, serverAddr, payload)
	if err != nil {
		t.Fatalf("DialTunnelWithPayload failed: %v", err)
	}
	defer conn.Close()

	// Read response
	response := make([]byte, len(payload))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, response); err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if string(response) != string(payload) {
		t.Errorf("Expected %q, got %q", string(payload), string(response))
	}

	t.Logf("DialTunnelWithPayload test passed")
}

// TestTunnelConn tests the TunnelConn wrapper
func TestTunnelConn(t *testing.T) {
	// Start echo server that parses domain header
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	defer listener.Close()

	echoAddr := listener.Addr().String()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Read domain header
				var domainLenBuf [2]byte
				if _, err := io.ReadFull(c, domainLenBuf[:]); err != nil {
					return
				}
				domainLen := binary.BigEndian.Uint16(domainLenBuf[:])
				domain := make([]byte, domainLen)
				if _, err := io.ReadFull(c, domain); err != nil {
					return
				}
				// Echo remaining data
				io.Copy(c, c)
			}(conn)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Test TunnelConn
	domain := "test.tunnel.com"
	conn, err := tunnelquic.DialTunnelConn(domain, echoAddr)
	if err != nil {
		t.Fatalf("DialTunnelConn failed: %v", err)
	}
	defer conn.Close()

	// Verify domain is stored
	if conn.Domain != domain {
		t.Errorf("Expected domain %q, got %q", domain, conn.Domain)
	}

	// Verify connection works
	testData := []byte("Test TunnelConn")
	if _, err := conn.Write(testData); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	response := make([]byte, len(testData))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, response); err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if string(response) != string(testData) {
		t.Errorf("Expected %q, got %q", string(testData), string(response))
	}

	t.Logf("TunnelConn test passed")
}
