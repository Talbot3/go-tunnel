package quic_test

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

	"github.com/Talbot3/go-tunnel/quic"
)

func TestProtocol_Name(t *testing.T) {
	tlsConfig := &tls.Config{NextProtos: []string{"quic-proxy"}}
	p := quic.New(tlsConfig, nil)
	if p.Name() != "quic" {
		t.Errorf("Expected name 'quic', got '%s'", p.Name())
	}
}

func TestProtocol_Forwarder(t *testing.T) {
	tlsConfig := &tls.Config{NextProtos: []string{"quic-proxy"}}
	p := quic.New(tlsConfig, nil)
	fwd := p.Forwarder()
	if fwd == nil {
		t.Error("Expected non-nil forwarder")
	}
}

func TestProtocol_ListenInvalidAddress(t *testing.T) {
	cert := generateTestCertificate(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-proxy"},
	}
	p := quic.New(tlsConfig, nil)

	_, err := p.Listen("invalid:address")
	if err == nil {
		t.Error("Expected error for invalid address")
	}
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

func TestProtocol_ListenAndAddr(t *testing.T) {
	cert := generateTestCertificate(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-proxy"},
	}
	p := quic.New(tlsConfig, nil)

	listener, err := p.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	if listener.Addr() == nil {
		t.Error("Expected non-nil address")
	}

	// Verify it's a UDP address
	addr := listener.Addr().String()
	if addr == "" {
		t.Error("Expected non-empty address string")
	}
}

func TestProtocol_DialInvalidAddress(t *testing.T) {
	cert := generateTestCertificate(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-proxy"},
		InsecureSkipVerify: true,
	}
	p := quic.New(tlsConfig, nil)

	ctx, cancel := contextWithTimeout(100 * time.Millisecond)
	defer cancel()

	// Dial to an address that won't respond
	_, err := p.Dial(ctx, "10.255.255.1:12345")
	if err == nil {
		t.Error("Expected error for non-routable address")
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

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
