package quic_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/Talbot3/go-tunnel/quic"
)

func TestMuxServer_Name(t *testing.T) {
	cert := generateTestCertificate(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-tunnel"},
	}

	server := quic.NewMuxServer(quic.MuxServerConfig{
		ListenAddr: ":0",
		TLSConfig:  tlsConfig,
	})

	if server.Name() != "quic-mux" {
		t.Errorf("Expected name 'quic-mux', got '%s'", server.Name())
	}
}

func TestMuxServer_StartInvalidAddress(t *testing.T) {
	cert := generateTestCertificate(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-tunnel"},
	}

	server := quic.NewMuxServer(quic.MuxServerConfig{
		ListenAddr: "invalid:address:format",
		TLSConfig:  tlsConfig,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := server.Start(ctx)
	if err == nil {
		t.Error("Expected error for invalid address")
		server.Stop()
	}
}

func TestMuxClient_InvalidServer(t *testing.T) {
	cert := generateTestCertificate(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-tunnel"},
		InsecureSkipVerify: true,
	}

	client := quic.NewMuxClient(quic.MuxClientConfig{
		ServerAddr: "10.255.255.1:12345",
		TLSConfig:  tlsConfig,
		LocalAddr:  "localhost:8080",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start returns immediately and runs connection loop in background
	err := client.Start(ctx)
	if err != nil {
		t.Error("Start should not return error immediately")
	}
	defer client.Stop()

	// Wait for context to be done (connection will fail in background)
	<-ctx.Done()
}

func TestDefaultConfig(t *testing.T) {
	cfg := quic.DefaultConfig()
	if cfg == nil {
		t.Fatal("Expected non-nil config")
	}
	if cfg.MaxIncomingStreams <= 0 {
		t.Error("Expected positive MaxIncomingStreams")
	}
	if cfg.KeepAlivePeriod <= 0 {
		t.Error("Expected positive KeepAlivePeriod")
	}
}

func TestIsQUICClosedErr(t *testing.T) {
	// nil error
	if !quic.IsQUICClosedErr(nil) {
		t.Error("Expected true for nil error")
	}
}

func TestMuxServer_GetStats(t *testing.T) {
	cert := generateTestCertificate(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-tunnel"},
	}

	server := quic.NewMuxServer(quic.MuxServerConfig{
		ListenAddr: "127.0.0.1:0",
		TLSConfig:  tlsConfig,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := server.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	stats := server.GetStats()
	if stats.ActiveTunnels != 0 {
		t.Errorf("Expected 0 active tunnels, got %d", stats.ActiveTunnels)
	}
}

func TestMuxServer_MuxClient_Integration(t *testing.T) {
	cert := generateTestCertificate(t)
	serverTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-tunnel"},
	}

	// Start local echo server
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	defer echoListener.Close()

	echoAddr := echoListener.Addr().String()
	go runEchoServer(echoListener)

	// Start QUIC server
	server := quic.NewMuxServer(quic.MuxServerConfig{
		ListenAddr:     "127.0.0.1:0",
		TLSConfig:      serverTLSConfig,
		PortRangeStart: 20000,
		PortRangeEnd:   20100,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Get server address
	serverAddr := server.Addr()
	if serverAddr == nil {
		t.Fatal("Server address is nil")
	}

	// Start QUIC client
	clientTLSConfig := &tls.Config{
		NextProtos:         []string{"quic-tunnel"},
		InsecureSkipVerify: true,
	}

	client := quic.NewMuxClient(quic.MuxClientConfig{
		ServerAddr: serverAddr.String(),
		TLSConfig:  clientTLSConfig,
		LocalAddr:  echoAddr,
		AuthToken:  "test-token",
	})

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop()

	// Wait for registration
	time.Sleep(200 * time.Millisecond)

	stats := client.GetStats()
	t.Logf("Client stats: ActiveConns=%d, TotalConns=%d", stats.ActiveConns, stats.TotalConns)
}

func runEchoServer(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			buf := make([]byte, 1024)
			for {
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				c.Write(buf[:n])
			}
		}(conn)
	}
}

// Helper functions

func generateTestCertificate(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
}
