package tcp_test

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Talbot3/go-tunnel/tcp"
)

func TestProtocol_Name(t *testing.T) {
	p := tcp.New()
	if p.Name() != "tcp" {
		t.Errorf("Expected name 'tcp', got '%s'", p.Name())
	}
}

func TestProtocol_ListenAndDial(t *testing.T) {
	p := tcp.New()

	// Create listener
	listener, err := p.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Start echo server
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn) // Echo
	}()

	// Dial and test
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := p.Dial(ctx, addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	// Test data transfer
	testData := []byte("Hello, TCP!")
	if _, err := conn.Write(testData); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	buf := make([]byte, len(testData))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if string(buf) != string(testData) {
		t.Errorf("Expected %q, got %q", string(testData), string(buf))
	}

	conn.Close()
	wg.Wait()
}

func TestProtocol_DialTimeout(t *testing.T) {
	p := tcp.New()

	// Try to dial to an address that won't respond
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Use a non-routable IP to ensure timeout
	_, err := p.Dial(ctx, "10.255.255.1:12345")
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}

func TestProtocol_Forwarder(t *testing.T) {
	p := tcp.New()
	fwd := p.Forwarder()
	if fwd == nil {
		t.Error("Expected non-nil forwarder")
	}
}

func TestProtocol_ListenInvalidAddress(t *testing.T) {
	p := tcp.New()

	// Try to listen on an invalid address
	_, err := p.Listen("invalid:address")
	if err == nil {
		t.Error("Expected error for invalid address")
	}
}

func TestProtocol_MultipleConnections(t *testing.T) {
	p := tcp.New()

	listener, err := p.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Start echo server
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Make multiple connections
	numConns := 5
	var wg sync.WaitGroup
	wg.Add(numConns)

	for i := 0; i < numConns; i++ {
		go func(id int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			conn, err := p.Dial(ctx, addr)
			if err != nil {
				t.Errorf("Connection %d failed: %v", id, err)
				return
			}
			defer conn.Close()

			testData := []byte("Test message")
			if _, err := conn.Write(testData); err != nil {
				t.Errorf("Connection %d write failed: %v", id, err)
				return
			}

			buf := make([]byte, len(testData))
			if _, err := io.ReadFull(conn, buf); err != nil {
				t.Errorf("Connection %d read failed: %v", id, err)
				return
			}
		}(i)
	}

	wg.Wait()
}
