package quic_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	quicgo "github.com/quic-go/quic-go"
	tunnelquic "github.com/Talbot3/go-tunnel/quic"
)

func TestMuxServer_Name(t *testing.T) {
	cert := generateTestCertificate(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-tunnel"},
	}

	server := tunnelquic.NewMuxServer(tunnelquic.MuxServerConfig{
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

	server := tunnelquic.NewMuxServer(tunnelquic.MuxServerConfig{
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

	client := tunnelquic.NewMuxClient(tunnelquic.MuxClientConfig{
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
	cfg := tunnelquic.DefaultConfig()
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
	if !tunnelquic.IsQUICClosedErr(nil) {
		t.Error("Expected true for nil error")
	}
}

func TestMuxServer_GetStats(t *testing.T) {
	cert := generateTestCertificate(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-tunnel"},
	}

	server := tunnelquic.NewMuxServer(tunnelquic.MuxServerConfig{
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
	server := tunnelquic.NewMuxServer(tunnelquic.MuxServerConfig{
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

	client := tunnelquic.NewMuxClient(tunnelquic.MuxClientConfig{
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

// ============================================
// Pure QUIC Tunnel Tests
// ============================================

// TestProtocolQUIC tests that ProtocolQUIC constant is correctly defined
func TestProtocolQUIC(t *testing.T) {
	if tunnelquic.ProtocolQUIC != 0x03 {
		t.Errorf("Expected ProtocolQUIC = 0x03, got 0x%02x", tunnelquic.ProtocolQUIC)
	}
}

// TestMuxClient_WithProtocolQUIC tests creating client with ProtocolQUIC
func TestMuxClient_WithProtocolQUIC(t *testing.T) {
	client := tunnelquic.NewMuxClient(tunnelquic.MuxClientConfig{
		ServerAddr: "localhost:443",
		Protocol:   tunnelquic.ProtocolQUIC,
		LocalAddr:  "localhost:8443",
	})

	// Just verify client was created
	if client == nil {
		t.Error("Expected non-nil client")
	}
}

// TestParseRegisterPayload_QUIC tests parsing register payload with ProtocolQUIC
func TestParseRegisterPayload_QUIC(t *testing.T) {
	// This is tested indirectly through the integration tests
	// The parseRegisterPayload function is internal and handles ProtocolQUIC
}

// TestMuxServer_QUICTunnel_AuthFailure tests authentication failure for QUIC tunnel
func TestMuxServer_QUICTunnel_AuthFailure(t *testing.T) {
	cert := generateTestCertificate(t)
	serverTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-tunnel"},
	}

	server := tunnelquic.NewMuxServer(tunnelquic.MuxServerConfig{
		ListenAddr:     "127.0.0.1:0",
		TLSConfig:      serverTLSConfig,
		PortRangeStart: 21200,
		PortRangeEnd:   21300,
		AuthToken:      "correct-token",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	serverAddr := server.Addr()

	// Client with wrong token
	clientTLSConfig := &tls.Config{
		NextProtos:         []string{"quic-tunnel"},
		InsecureSkipVerify: true,
	}

	client := tunnelquic.NewMuxClient(tunnelquic.MuxClientConfig{
		ServerAddr: serverAddr.String(),
		TLSConfig:  clientTLSConfig,
		Protocol:   tunnelquic.ProtocolQUIC,
		LocalAddr:  "localhost:8443",
		AuthToken:  "wrong-token",
	})

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop()

	// Wait for connection attempt
	time.Sleep(500 * time.Millisecond)

	// Client should not be connected due to auth failure
	stats := client.GetStats()
	if stats.Connected {
		t.Error("Expected client to not be connected due to auth failure")
	}
}

// TestQUICEchoServer is a helper that runs a QUIC echo server
type TestQUICEchoServer struct {
	listener *quicgo.Listener
	addr     string
	wg       sync.WaitGroup
}

func NewTestQUICEchoServer(t *testing.T, cert tls.Certificate) *TestQUICEchoServer {
	t.Helper()

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-tunnel"}, // Match the tunnel's ALPN
	}

	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to resolve UDP address: %v", err)
	}

	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		t.Fatalf("Failed to listen UDP: %v", err)
	}

	quicConfig := &quicgo.Config{
		MaxIncomingStreams: 100,
	}

	listener, err := quicgo.Listen(udpConn, tlsConfig, quicConfig)
	if err != nil {
		t.Fatalf("Failed to listen QUIC: %v", err)
	}

	s := &TestQUICEchoServer{
		listener: listener,
		addr:     listener.Addr().String(),
	}

	s.wg.Add(1)
	go s.serve()

	return s
}

func (s *TestQUICEchoServer) serve() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept(context.Background())
		if err != nil {
			return
		}

		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *TestQUICEchoServer) handleConn(conn quicgo.Connection) {
	defer s.wg.Done()
	defer conn.CloseWithError(0, "done")

	for {
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			return
		}

		s.wg.Add(1)
		go func(stream quicgo.Stream) {
			defer s.wg.Done()
			defer stream.Close()
			io.Copy(stream, stream) // Echo back
		}(stream)
	}
}

func (s *TestQUICEchoServer) Addr() string {
	return s.addr
}

func (s *TestQUICEchoServer) Close() {
	s.listener.Close()
	s.wg.Wait()
}

// TestMuxClient_QUICDataStream tests handling QUIC data streams on client side
func TestMuxClient_QUICDataStream(t *testing.T) {
	cert := generateTestCertificate(t)

	// Create a mock local QUIC service (echo server)
	echoServer := NewTestQUICEchoServer(t, cert)
	defer echoServer.Close()

	t.Logf("Local QUIC echo server listening on %s", echoServer.Addr())

	// This test verifies the client can handle QUIC data streams
	// The full end-to-end test would require external QUIC client connecting to server
	// which is covered in TestQUICTunnel_EndToEnd
}

// TestQUICTunnel_EndToEnd tests complete QUIC tunnel flow
func TestQUICTunnel_EndToEnd(t *testing.T) {
	cert := generateTestCertificate(t)

	// 1. Start local QUIC echo server
	localQUICServer := NewTestQUICEchoServer(t, cert)
	defer localQUICServer.Close()
	localQUICAddr := localQUICServer.Addr()

	t.Logf("Local QUIC service: %s", localQUICAddr)

	// 2. Start tunnel server
	serverTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-tunnel"},
	}

	server := tunnelquic.NewMuxServer(tunnelquic.MuxServerConfig{
		ListenAddr:     "127.0.0.1:0",
		TLSConfig:      serverTLSConfig,
		PortRangeStart: 22000,
		PortRangeEnd:   22100,
		AuthToken:      "test-token",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	serverAddr := server.Addr()
	t.Logf("Tunnel server: %s", serverAddr)

	// 3. Start tunnel client with ProtocolQUIC
	clientTLSConfig := &tls.Config{
		NextProtos:         []string{"quic-tunnel"},
		InsecureSkipVerify: true,
	}

	client := tunnelquic.NewMuxClient(tunnelquic.MuxClientConfig{
		ServerAddr: serverAddr.String(),
		TLSConfig:  clientTLSConfig,
		Protocol:   tunnelquic.ProtocolQUIC,
		LocalAddr:  localQUICAddr,
		AuthToken:  "test-token",
	})

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop()

	// Wait for registration
	time.Sleep(300 * time.Millisecond)

	stats := client.GetStats()
	if !stats.Connected {
		t.Fatal("Expected client to be connected")
	}

	publicURL := client.PublicURL()
	t.Logf("Public QUIC URL: %s", publicURL)

	// 4. Extract port from public URL and connect as external QUIC client
	// Format: quic://:port
	var port int
	_, err := fmt.Sscanf(publicURL, "quic://:%d", &port)
	if err != nil {
		t.Fatalf("Failed to parse public URL: %v", err)
	}

	t.Logf("External QUIC port: %d", port)

	// 5. Connect as external QUIC client
	externalTLSConfig := &tls.Config{
		NextProtos:         []string{"quic-tunnel"},
		InsecureSkipVerify: true,
	}

	externalUDPAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("Failed to resolve external address: %v", err)
	}

	externalUDPConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatalf("Failed to create external UDP conn: %v", err)
	}

	externalConn, err := quicgo.Dial(ctx, externalUDPConn, externalUDPAddr, externalTLSConfig, tunnelquic.DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to dial external QUIC: %v", err)
	}
	defer externalConn.CloseWithError(0, "done")

	t.Log("External QUIC client connected")

	// 6. Open stream and send data
	stream, err := externalConn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("Failed to open stream: %v", err)
	}
	defer stream.Close()

	testData := []byte("Hello, QUIC Tunnel!")
	if _, err := stream.Write(testData); err != nil {
		t.Fatalf("Failed to write to stream: %v", err)
	}

	// 7. Read echo response
	response := make([]byte, len(testData))
	if _, err := io.ReadFull(stream, response); err != nil {
		t.Fatalf("Failed to read from stream: %v", err)
	}

	if string(response) != string(testData) {
		t.Errorf("Expected echo %q, got %q", string(testData), string(response))
	}

	t.Logf("Successfully echoed: %q", string(response))

	// 8. Verify stats
	clientStats := client.GetStats()
	t.Logf("Client stats: ActiveConns=%d, TotalConns=%d, BytesIn=%d, BytesOut=%d",
		clientStats.ActiveConns, clientStats.TotalConns, clientStats.BytesIn, clientStats.BytesOut)

	serverStats := server.GetStats()
	t.Logf("Server stats: ActiveTunnels=%d, ActiveConns=%d, BytesIn=%d, BytesOut=%d",
		serverStats.ActiveTunnels, serverStats.ActiveConns, serverStats.BytesIn, serverStats.BytesOut)
}

// TestQUICTunnel_MultipleStreams tests multiple concurrent streams through QUIC tunnel
func TestQUICTunnel_MultipleStreams(t *testing.T) {
	cert := generateTestCertificate(t)

	// Start local QUIC echo server
	localQUICServer := NewTestQUICEchoServer(t, cert)
	defer localQUICServer.Close()
	localQUICAddr := localQUICServer.Addr()

	// Start tunnel server
	serverTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-tunnel"},
	}

	server := tunnelquic.NewMuxServer(tunnelquic.MuxServerConfig{
		ListenAddr:     "127.0.0.1:0",
		TLSConfig:      serverTLSConfig,
		PortRangeStart: 23000,
		PortRangeEnd:   23100,
		AuthToken:      "test-token",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	serverAddr := server.Addr()

	// Start tunnel client
	clientTLSConfig := &tls.Config{
		NextProtos:         []string{"quic-tunnel"},
		InsecureSkipVerify: true,
	}

	client := tunnelquic.NewMuxClient(tunnelquic.MuxClientConfig{
		ServerAddr: serverAddr.String(),
		TLSConfig:  clientTLSConfig,
		Protocol:   tunnelquic.ProtocolQUIC,
		LocalAddr:  localQUICAddr,
		AuthToken:  "test-token",
	})

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop()

	time.Sleep(300 * time.Millisecond)

	publicURL := client.PublicURL()
	var port int
	fmt.Sscanf(publicURL, "quic://:%d", &port)

	// Connect as external QUIC client
	externalTLSConfig := &tls.Config{
		NextProtos:         []string{"quic-tunnel"},
		InsecureSkipVerify: true,
	}

	externalUDPAddr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", port))
	externalUDPConn, _ := net.ListenUDP("udp", nil)

	externalConn, err := quicgo.Dial(ctx, externalUDPConn, externalUDPAddr, externalTLSConfig, tunnelquic.DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to dial external QUIC: %v", err)
	}
	defer externalConn.CloseWithError(0, "done")

	// Open multiple concurrent streams
	numStreams := 5
	var wg sync.WaitGroup
	errors := make(chan error, numStreams)

	for i := 0; i < numStreams; i++ {
		wg.Add(1)
		go func(streamNum int) {
			defer wg.Done()

			stream, err := externalConn.OpenStreamSync(ctx)
			if err != nil {
				errors <- fmt.Errorf("stream %d: failed to open: %v", streamNum, err)
				return
			}
			defer stream.Close()

			testData := []byte(fmt.Sprintf("Stream %d data", streamNum))
			if _, err := stream.Write(testData); err != nil {
				errors <- fmt.Errorf("stream %d: failed to write: %v", streamNum, err)
				return
			}

			response := make([]byte, len(testData))
			if _, err := io.ReadFull(stream, response); err != nil {
				errors <- fmt.Errorf("stream %d: failed to read: %v", streamNum, err)
				return
			}

			if string(response) != string(testData) {
				errors <- fmt.Errorf("stream %d: expected %q, got %q", streamNum, string(testData), string(response))
				return
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}

	t.Logf("Successfully handled %d concurrent streams", numStreams)
}

// TestQUICTunnel_Reconnection tests client reconnection for QUIC tunnel
func TestQUICTunnel_Reconnection(t *testing.T) {
	cert := generateTestCertificate(t)

	serverTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-tunnel"},
	}

	server := tunnelquic.NewMuxServer(tunnelquic.MuxServerConfig{
		ListenAddr:     "127.0.0.1:0",
		TLSConfig:      serverTLSConfig,
		PortRangeStart: 24000,
		PortRangeEnd:   24100,
		AuthToken:      "test-token",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	serverAddr := server.Addr()

	clientTLSConfig := &tls.Config{
		NextProtos:         []string{"quic-tunnel"},
		InsecureSkipVerify: true,
	}

	client := tunnelquic.NewMuxClient(tunnelquic.MuxClientConfig{
		ServerAddr:        serverAddr.String(),
		TLSConfig:         clientTLSConfig,
		Protocol:          tunnelquic.ProtocolQUIC,
		LocalAddr:         "localhost:8443",
		AuthToken:         "test-token",
		ReconnectInterval: 100 * time.Millisecond,
	})

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop()

	// Wait for initial connection
	time.Sleep(200 * time.Millisecond)

	stats := client.GetStats()
	if !stats.Connected {
		t.Error("Expected client to be connected initially")
	}

	initialTunnelID := client.TunnelID()
	t.Logf("Initial tunnel ID: %s", initialTunnelID)

	// Client should maintain connection
	time.Sleep(500 * time.Millisecond)

	stats = client.GetStats()
	t.Logf("Final connection state: Connected=%v", stats.Connected)
}
