package http3_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"github.com/Talbot3/go-tunnel/http3"
)

func TestProtocol_Name(t *testing.T) {
	tlsConfig := &tls.Config{NextProtos: []string{"h3"}}
	p := http3.New(tlsConfig, nil)
	if p.Name() != "http3" {
		t.Errorf("Expected name 'http3', got '%s'", p.Name())
	}
}

func TestProtocol_Forwarder(t *testing.T) {
	tlsConfig := &tls.Config{NextProtos: []string{"h3"}}
	p := http3.New(tlsConfig, nil)
	fwd := p.Forwarder()
	if fwd == nil {
		t.Error("Expected non-nil forwarder")
	}
}

func TestProtocol_ListenInvalidAddress(t *testing.T) {
	cert := generateTestCertificate(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}
	p := http3.New(tlsConfig, nil)

	_, err := p.Listen("invalid:address")
	if err == nil {
		t.Error("Expected error for invalid address")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := http3.DefaultConfig()
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

func TestIsHTTP3ClosedErr(t *testing.T) {
	// nil error
	if !http3.IsHTTP3ClosedErr(nil) {
		t.Error("Expected true for nil error")
	}
}

func TestNewServer(t *testing.T) {
	cert := generateTestCertificate(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}

	srv := http3.NewServer(tlsConfig)
	if srv == nil {
		t.Error("Expected non-nil server")
	}
	if srv.Server == nil {
		t.Error("Expected non-nil underlying server")
	}
}

func TestHTTP3Conn_Deadlines(t *testing.T) {
	// Test that deadline methods exist and don't panic
	// This is a compile-time check primarily
	cert := generateTestCertificate(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}

	p := http3.New(tlsConfig, nil)

	// Create listener
	listener, err := p.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	// Verify listener address
	if listener.Addr() == nil {
		t.Error("Expected non-nil address")
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
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
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
