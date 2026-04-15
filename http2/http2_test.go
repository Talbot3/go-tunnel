package http2_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"github.com/Talbot3/go-tunnel/http2"
)

func TestProtocol_Name(t *testing.T) {
	tlsConfig := &tls.Config{}
	p := http2.New(tlsConfig)
	if p.Name() != "http2" {
		t.Errorf("Expected name 'http2', got '%s'", p.Name())
	}
}

func TestProtocol_Forwarder(t *testing.T) {
	tlsConfig := &tls.Config{}
	p := http2.New(tlsConfig)
	fwd := p.Forwarder()
	if fwd == nil {
		t.Error("Expected non-nil forwarder")
	}
}

func TestProtocol_Listen(t *testing.T) {
	tlsConfig := &tls.Config{}
	p := http2.New(tlsConfig)

	listener, err := p.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	if listener.Addr() == nil {
		t.Error("Expected non-nil address")
	}
}

func TestProtocol_DialWithoutTLS(t *testing.T) {
	p := http2.New(nil)

	ctx, cancel := createTestContext()
	defer cancel()

	// Dial without TLS config should work (plain TCP)
	conn, err := p.Dial(ctx, "127.0.0.1:80")
	if err != nil {
		// Connection may fail, but it shouldn't panic
		t.Logf("Dial returned error (expected for non-existent server): %v", err)
	}
	if conn != nil {
		conn.Close()
	}
}

func TestProtocol_DialWithTLS(t *testing.T) {
	cert := generateTestCertificate(t)
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	p := http2.New(tlsConfig)

	ctx, cancel := createTestContext()
	defer cancel()

	// Dial with TLS config
	conn, err := p.Dial(ctx, "127.0.0.1:443")
	if err != nil {
		t.Logf("Dial returned error (expected for non-existent server): %v", err)
	}
	if conn != nil {
		conn.Close()
	}
}

func TestConfigureServer(t *testing.T) {
	// Just verify it doesn't panic
	http2.ConfigureServer(nil)
}

func TestConfigureTransport(t *testing.T) {
	// Just verify it doesn't panic
	http2.ConfigureTransport(nil)
}

// Helper functions

func createTestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 1*time.Second)
}

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
