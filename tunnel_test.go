package tunnel_test

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Talbot3/go-tunnel"
	"github.com/Talbot3/go-tunnel/tcp"
)

// TestTunnel_TCPForward tests basic TCP forwarding.
func TestTunnel_TCPForward(t *testing.T) {
	// Create a backend server
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create backend listener: %v", err)
	}
	defer backendListener.Close()

	backendAddr := backendListener.Addr().String()

	// Start backend server
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Echo server
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return
			}
		}
	}()

	// Create tunnel
	cfg := tunnel.Config{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: backendAddr,
	}

	tun, err := tunnel.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create tunnel: %v", err)
	}

	tun.SetProtocol(tcp.New())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tun.Start(ctx); err != nil {
		t.Fatalf("Failed to start tunnel: %v", err)
	}
	defer tun.Stop()

	// Give the tunnel time to start
	time.Sleep(10 * time.Millisecond)

	// Connect through tunnel
	conn, err := net.Dial("tcp", tun.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect to tunnel: %v", err)
	}
	defer conn.Close()

	// Send data and verify echo
	testData := []byte("Hello, Tunnel!")
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

	// Check stats
	stats := tun.Stats()
	if stats.Connections() != 1 {
		t.Errorf("Expected 1 connection, got %d", stats.Connections())
	}
}

// TestTunnel_MultipleConnections tests multiple concurrent connections.
func TestTunnel_MultipleConnections(t *testing.T) {
	// Create a backend server
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create backend listener: %v", err)
	}
	defer backendListener.Close()

	backendAddr := backendListener.Addr().String()

	// Start backend server
	go func() {
		for {
			conn, err := backendListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c) // Echo
			}(conn)
		}
	}()

	// Create tunnel
	cfg := tunnel.Config{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: backendAddr,
	}

	tun, err := tunnel.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create tunnel: %v", err)
	}

	tun.SetProtocol(tcp.New())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tun.Start(ctx); err != nil {
		t.Fatalf("Failed to start tunnel: %v", err)
	}
	defer tun.Stop()

	time.Sleep(10 * time.Millisecond)

	// Make multiple concurrent connections
	numConns := 10
	var wg sync.WaitGroup
	wg.Add(numConns)

	for i := 0; i < numConns; i++ {
		go func(id int) {
			defer wg.Done()

			conn, err := net.Dial("tcp", tun.Addr().String())
			if err != nil {
				t.Errorf("Failed to connect: %v", err)
				return
			}
			defer conn.Close()

			testData := []byte("Test message from connection")
			if _, err := conn.Write(testData); err != nil {
				t.Errorf("Failed to write: %v", err)
				return
			}

			buf := make([]byte, len(testData))
			if _, err := io.ReadFull(conn, buf); err != nil {
				t.Errorf("Failed to read: %v", err)
				return
			}

			if string(buf) != string(testData) {
				t.Errorf("Data mismatch")
			}
		}(i)
	}

	wg.Wait()

	// Check stats
	stats := tun.Stats()
	if stats.Connections() != int64(numConns) {
		t.Errorf("Expected %d connections, got %d", numConns, stats.Connections())
	}
}

// TestTunnel_Stats tests statistics tracking.
func TestTunnel_Stats(t *testing.T) {
	cfg := tunnel.Config{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: "127.0.0.1:12345", // Dummy target
	}

	tun, err := tunnel.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create tunnel: %v", err)
	}

	stats := tun.Stats()

	// Test initial state
	if stats.Connections() != 0 {
		t.Errorf("Expected 0 connections initially")
	}
	if stats.BytesSent() != 0 {
		t.Errorf("Expected 0 bytes sent initially")
	}
	if stats.BytesReceived() != 0 {
		t.Errorf("Expected 0 bytes received initially")
	}
	if stats.Errors() != 0 {
		t.Errorf("Expected 0 errors initially")
	}

	// Test uptime
	if stats.Uptime() < 0 {
		t.Errorf("Uptime should not be negative")
	}

	// Test Reset
	time.Sleep(10 * time.Millisecond)
	stats.Reset()

	if stats.Connections() != 0 {
		t.Errorf("Expected 0 connections after reset")
	}
	if stats.Uptime() > 10*time.Millisecond {
		t.Errorf("Uptime should be small after reset")
	}
}

// TestTunnel_Stop tests graceful shutdown.
func TestTunnel_Stop(t *testing.T) {
	// Create a backend server
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create backend listener: %v", err)
	}
	defer backendListener.Close()

	backendAddr := backendListener.Addr().String()

	// Start backend server
	go func() {
		for {
			conn, err := backendListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Create tunnel
	cfg := tunnel.Config{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: backendAddr,
	}

	tun, err := tunnel.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create tunnel: %v", err)
	}

	tun.SetProtocol(tcp.New())

	ctx, cancel := context.WithCancel(context.Background())

	if err := tun.Start(ctx); err != nil {
		t.Fatalf("Failed to start tunnel: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Stop the tunnel
	if err := tun.Stop(); err != nil {
		t.Errorf("Failed to stop tunnel: %v", err)
	}

	cancel()

	// Verify we can't connect anymore
	time.Sleep(10 * time.Millisecond)
	_, err = net.Dial("tcp", tun.Addr().String())
	if err == nil {
		t.Error("Expected connection to fail after tunnel stopped")
	}
}

// TestHandlePair tests the HandlePair convenience function.
func TestHandlePair(t *testing.T) {
	// Create two pipes
	r1, w1 := net.Pipe()
	r2, w2 := net.Pipe()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		tunnel.HandlePair(r1, w2)
	}()

	// Write to w1, should appear on r2
	testData := []byte("Test HandlePair")
	if _, err := w1.Write(testData); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	buf := make([]byte, len(testData))
	if _, err := io.ReadFull(r2, buf); err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if string(buf) != string(testData) {
		t.Errorf("Expected %q, got %q", string(testData), string(buf))
	}

	// Close connections
	w1.Close()
	r2.Close()

	wg.Wait()
}

// TestForward tests the Forward function.
func TestForward(t *testing.T) {
	// Create two pipes
	r1, w1 := net.Pipe()
	r2, w2 := net.Pipe()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		tunnel.Forward(r1, w2)
	}()

	// Write to w1, should appear on r2
	testData := []byte("Test Forward")
	if _, err := w1.Write(testData); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	buf := make([]byte, len(testData))
	if _, err := io.ReadFull(r2, buf); err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if string(buf) != string(testData) {
		t.Errorf("Expected %q, got %q", string(testData), string(buf))
	}

	// Close to end the forward
	w1.Close()
	r2.Close()

	wg.Wait()
}
