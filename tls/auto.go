// Package tls provides automatic TLS certificate management using ACME protocol.
//
// This package integrates with certmagic to provide automatic certificate
// issuance and renewal from Let's Encrypt, ZeroSSL, and other ACME CAs.
//
// # Features
//
// - Automatic certificate issuance via ACME protocol
// - Automatic renewal before expiration
// - Support for HTTP-01 and DNS-01 challenge types
// - Multiple CA support with automatic failover
// - OCSP stapling for improved TLS performance
//
// # Example
//
//	mgr := tls.NewAutoManager(AutoManagerConfig{Email: "admin@example.com"})
//	mgr.AddDomains("example.com", "www.example.com")
//	tlsConfig := mgr.TLSConfig()
//	server := &http.Server{TLSConfig: tlsConfig}
package tls

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"sync"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/libdns"
)

// DNSProvider is the interface for DNS-01 challenge providers.
type DNSProvider interface {
	libdns.RecordAppender
	libdns.RecordDeleter
}

// AutoManager manages automatic TLS certificates using ACME.
type AutoManager struct {
	mu       sync.RWMutex
	cfg      *certmagic.Config
	issuer   *certmagic.ACMEIssuer
	domains  []string
	email    string
	ca       string
	dns01    bool
	httpPort int
	tlsPort  int

	// DNS provider for DNS-01 challenge
	dnsProvider DNSProvider

	// Storage backend
	storage certmagic.Storage
}

// AutoManagerConfig holds configuration for AutoManager.
type AutoManagerConfig struct {
	// Email for ACME account registration
	Email string

	// CA directory URL (default: Let's Encrypt Production)
	CA string

	// Use DNS-01 challenge instead of HTTP-01
	UseDNS01 bool

	// DNS provider for DNS-01 challenge
	DNSProvider DNSProvider

	// Custom storage backend (default: filesystem)
	Storage certmagic.Storage

	// HTTP port for HTTP-01 challenge (default: 80)
	HTTPPort int

	// TLS port for TLS-ALPN-01 challenge (default: 443)
	TLSPort int

	// Agree to CA Terms of Service
	AgreeTerms bool

	// Enable staging mode (uses Let's Encrypt staging CA)
	Staging bool
}

// NewAutoManager creates a new automatic certificate manager.
func NewAutoManager(cfg AutoManagerConfig) *AutoManager {
	m := &AutoManager{
		email:    cfg.Email,
		dns01:    cfg.UseDNS01,
		httpPort: cfg.HTTPPort,
		tlsPort:  cfg.TLSPort,
	}

	if m.httpPort == 0 {
		m.httpPort = 80
	}
	if m.tlsPort == 0 {
		m.tlsPort = 443
	}

	// Set CA URL
	if cfg.Staging {
		m.ca = certmagic.LetsEncryptStagingCA
	} else if cfg.CA != "" {
		m.ca = cfg.CA
	} else {
		m.ca = certmagic.LetsEncryptProductionCA
	}

	// Initialize certmagic config
	m.cfg = certmagic.NewDefault()

	// Set storage
	if cfg.Storage != nil {
		m.cfg.Storage = cfg.Storage
	} else {
		// Use default file storage
		m.cfg.Storage = &certmagic.FileStorage{
			Path: getDataDir(),
		}
	}

	// Create ACME issuer
	issuerConfig := certmagic.ACMEIssuer{
		Email:  cfg.Email,
		Agreed: cfg.AgreeTerms,
		CA:     m.ca,
	}

	// Configure DNS-01 if enabled
	if cfg.UseDNS01 && cfg.DNSProvider != nil {
		issuerConfig.DNS01Solver = &certmagic.DNS01Solver{
			DNSManager: certmagic.DNSManager{
				DNSProvider: cfg.DNSProvider,
			},
		}
	}

	m.issuer = certmagic.NewACMEIssuer(m.cfg, issuerConfig)
	m.cfg.Issuers = []certmagic.Issuer{m.issuer}

	return m
}

// getDataDir returns the data directory for certificate storage.
func getDataDir() string {
	// Try XDG_DATA_HOME first
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir + "/go-tunnel/certs"
	}

	// Try HOME
	if dir, err := os.UserHomeDir(); err == nil {
		return dir + "/.local/share/go-tunnel/certs"
	}

	// Fallback to /tmp
	return "/tmp/go-tunnel/certs"
}

// AddDomains adds domains for certificate management.
func (m *AutoManager) AddDomains(domains ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.domains = append(m.domains, domains...)

	// Manage certificates for these domains
	if err := m.cfg.ManageSync(context.Background(), domains); err != nil {
		return fmt.Errorf("failed to manage certificates: %w", err)
	}

	return nil
}

// RemoveDomains removes domains from certificate management.
func (m *AutoManager) RemoveDomains(domains ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Filter out removed domains
	newDomains := make([]string, 0, len(m.domains))
	removeMap := make(map[string]bool)
	for _, d := range domains {
		removeMap[d] = true
	}
	for _, d := range m.domains {
		if !removeMap[d] {
			newDomains = append(newDomains, d)
		}
	}
	m.domains = newDomains
}

// TLSConfig returns a tls.Config that uses managed certificates.
func (m *AutoManager) TLSConfig() *tls.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.cfg.TLSConfig()
}

// GetCertificate returns the certificate for the given domain.
func (m *AutoManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return m.cfg.GetCertificate(hello)
}

// RenewAll forces renewal of all managed certificates.
func (m *AutoManager) RenewAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, domain := range m.domains {
		if err := m.cfg.RenewCertSync(ctx, domain, false); err != nil {
			return fmt.Errorf("failed to renew certificate for %s: %w", domain, err)
		}
	}
	return nil
}

// Revoke revokes the certificate for the given domain.
func (m *AutoManager) Revoke(ctx context.Context, domain string, reason int) error {
	_, err := m.cfg.CacheManagedCertificate(ctx, domain)
	if err != nil {
		return fmt.Errorf("failed to get certificate: %w", err)
	}
	return m.issuer.Revoke(ctx, certmagic.CertificateResource{
		SANs: []string{domain},
	}, reason)
}

// Domains returns all managed domains.
func (m *AutoManager) Domains() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string{}, m.domains...)
}

// CacheStatus returns the certificate cache status.
func (m *AutoManager) CacheStatus() map[string]*CertStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]*CertStatus)
	for _, domain := range m.domains {
		cert, err := m.cfg.CacheManagedCertificate(context.Background(), domain)
		if err != nil {
			status[domain] = &CertStatus{
				Domain:  domain,
				Valid:   false,
				Message: err.Error(),
			}
			continue
		}

		status[domain] = &CertStatus{
			Domain:    domain,
			Valid:     true,
			NotBefore: cert.Leaf.NotBefore.String(),
			NotAfter:  cert.Leaf.NotAfter.String(),
			Issuer:    cert.Leaf.Issuer.CommonName,
		}
	}
	return status
}

// CertStatus represents the status of a certificate.
type CertStatus struct {
	Domain    string `json:"domain"`
	Valid     bool   `json:"valid"`
	NotBefore string `json:"not_before,omitempty"`
	NotAfter  string `json:"not_after,omitempty"`
	Issuer    string `json:"issuer,omitempty"`
	Message   string `json:"message,omitempty"`
}

// SetStorage sets a custom storage backend.
func (m *AutoManager) SetStorage(storage certmagic.Storage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Storage = storage
}

// SetDNSProvider sets the DNS provider for DNS-01 challenges.
func (m *AutoManager) SetDNSProvider(provider DNSProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dnsProvider = provider
	m.issuer.DNS01Solver = &certmagic.DNS01Solver{
		DNSManager: certmagic.DNSManager{
			DNSProvider: provider,
		},
	}
}

// SetHTTPPort sets the port for HTTP-01 challenges.
func (m *AutoManager) SetHTTPPort(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.httpPort = port
}

// SetTLSPort sets the port for TLS-ALPN-01 challenges.
func (m *AutoManager) SetTLSPort(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tlsPort = port
}

// UseStaging switches to Let's Encrypt staging CA.
func (m *AutoManager) UseStaging() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ca = certmagic.LetsEncryptStagingCA
	m.issuer.CA = m.ca
}

// UseProduction switches to Let's Encrypt production CA.
func (m *AutoManager) UseProduction() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ca = certmagic.LetsEncryptProductionCA
	m.issuer.CA = m.ca
}

// UseZeroSSL switches to ZeroSSL CA.
func (m *AutoManager) UseZeroSSL() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ca = certmagic.ZeroSSLProductionCA
	m.issuer.CA = m.ca
}

// DefaultAutoManager returns a default auto manager with sensible defaults.
func DefaultAutoManager(email string) *AutoManager {
	return NewAutoManager(AutoManagerConfig{
		Email:      email,
		AgreeTerms: true,
	})
}

// QuickSetup quickly sets up automatic TLS for the given domains.
// This is a convenience function for simple use cases.
func QuickSetup(email string, domains ...string) (*AutoManager, error) {
	mgr := DefaultAutoManager(email)
	if err := mgr.AddDomains(domains...); err != nil {
		return nil, err
	}
	return mgr, nil
}
