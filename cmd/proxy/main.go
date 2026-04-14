// Command proxy is the CLI entry point for the go-tunnel library.
//
// Usage:
//
//	proxy -protocol tcp -listen :8080 -target 127.0.0.1:80
//	proxy -config config.yaml
//	proxy -version
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Talbot3/go-tunnel"
	"github.com/Talbot3/go-tunnel/config"
	"github.com/Talbot3/go-tunnel/http2"
	"github.com/Talbot3/go-tunnel/http3"
	"github.com/Talbot3/go-tunnel/quic"
	autotls "github.com/Talbot3/go-tunnel/tls"
	"github.com/Talbot3/go-tunnel/tcp"
)

var (
	protocol    = flag.String("protocol", "tcp", "Protocol to use (tcp, http2, http3, quic)")
	listenAddr  = flag.String("listen", ":8080", "Address to listen on")
	targetAddr  = flag.String("target", "", "Target address to forward to")
	configFile  = flag.String("config", "", "Path to configuration file")
	certFile    = flag.String("cert", "", "TLS certificate file")
	keyFile     = flag.String("key", "", "TLS private key file")
	showVersion = flag.Bool("version", false, "Show version information")

	// Auto TLS flags
	autoTLS     = flag.Bool("auto-tls", false, "Enable automatic TLS certificate management")
	email       = flag.String("email", "", "Email for ACME account registration")
	domains     = flag.String("domains", "", "Comma-separated list of domains for auto TLS")
	dnsProvider = flag.String("dns-provider", "", "DNS provider for DNS-01 challenge (cloudflare, alidns, route53)")
	staging     = flag.Bool("staging", false, "Use Let's Encrypt staging CA")
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("go-tunnel version %s\n", tunnel.Version)
		os.Exit(0)
	}

	if *configFile != "" {
		runFromConfig(*configFile)
		return
	}

	if *targetAddr == "" {
		fmt.Fprintln(os.Stderr, "Error: target address is required")
		flag.Usage()
		os.Exit(1)
	}

	var tlsConfig *tls.Config
	var err error

	// Handle automatic TLS
	if *autoTLS {
		if *email == "" || *domains == "" {
			fmt.Fprintln(os.Stderr, "Error: --email and --domains are required for auto TLS")
			os.Exit(1)
		}

		mgr, err := setupAutoTLS()
		if err != nil {
			log.Fatalf("Failed to setup auto TLS: %v", err)
		}
		tlsConfig = mgr.TLSConfig()
	} else if *certFile != "" && *keyFile != "" {
		tlsConfig, err = loadTLSConfig(*certFile, *keyFile, "")
		if err != nil {
			log.Fatalf("Failed to load TLS config: %v", err)
		}
	}

	if err := runSingle(*protocol, *listenAddr, *targetAddr, tlsConfig); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func setupAutoTLS() (*autotls.AutoManager, error) {
	cfg := autotls.AutoManagerConfig{
		Email:      *email,
		AgreeTerms: true,
		Staging:    *staging,
	}

	// Configure DNS provider if specified
	if *dnsProvider != "" {
		cfg.UseDNS01 = true
		// Note: For production, import the specific libdns provider
		// and configure it with credentials from environment
		log.Printf("DNS-01 challenge enabled with provider: %s", *dnsProvider)
		log.Println("Note: Set provider credentials via environment variables")
	}

	mgr := autotls.NewAutoManager(cfg)

	// Parse domains
	domainList := splitDomains(*domains)
	if err := mgr.AddDomains(domainList...); err != nil {
		return nil, fmt.Errorf("failed to add domains: %w", err)
	}

	log.Printf("Auto TLS configured for domains: %v", domainList)
	return mgr, nil
}

func splitDomains(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func runFromConfig(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}

	var tlsConfig *tls.Config
	var autoTLSMgr *autotls.AutoManager

	// Check for auto TLS configuration
	if cfg.Options["auto_tls"] == "true" {
		email := cfg.Options["email"]
		domains := cfg.Options["domains"]
		if email != "" && domains != "" {
			autoTLSMgr = autotls.NewAutoManager(autotls.AutoManagerConfig{
				Email:      email,
				AgreeTerms: true,
				Staging:    cfg.Options["staging"] == "true",
			})
			if err := autoTLSMgr.AddDomains(splitDomains(domains)...); err != nil {
				log.Fatalf("Failed to setup auto TLS: %v", err)
			}
			tlsConfig = autoTLSMgr.TLSConfig()
			log.Printf("Auto TLS enabled for: %s", domains)
		}
	} else if cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
		tlsConfig, err = loadTLSConfig(cfg.TLS.CertFile, cfg.TLS.KeyFile, cfg.TLS.CAFile)
		if err != nil {
			log.Fatalf("Failed to load TLS config: %v", err)
		}
	}

	var tunnels []*tunnel.Tunnel
	for _, pc := range cfg.Protocols {
		if !pc.Enabled {
			continue
		}

		t, err := createTunnel(pc.Name, pc.Listen, pc.Target, tlsConfig)
		if err != nil {
			log.Printf("Failed to create tunnel for %s: %v", pc.Name, err)
			continue
		}

		if err := t.Start(context.Background()); err != nil {
			log.Printf("Failed to start tunnel for %s: %v", pc.Name, err)
			continue
		}

		tunnels = append(tunnels, t)
		log.Printf("Started %s tunnel: %s -> %s", pc.Name, pc.Listen, pc.Target)
	}

	if len(tunnels) == 0 {
		log.Fatal("No tunnels started")
	}

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		for _, t := range tunnels {
			t.Stop()
		}
		close(done)
	}()

	select {
	case <-done:
		log.Println("Shutdown complete")
	case <-ctx.Done():
		log.Println("Shutdown timeout exceeded, forcing exit")
	}
}

func runSingle(proto, listen, target string, tlsConfig *tls.Config) error {
	t, err := createTunnel(proto, listen, target, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to create tunnel: %w", err)
	}

	if err := t.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start tunnel: %w", err)
	}

	log.Printf("Started %s tunnel: %s -> %s", proto, listen, target)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		t.Stop()
		close(done)
	}()

	select {
	case <-done:
		log.Println("Shutdown complete")
	case <-ctx.Done():
		log.Println("Shutdown timeout exceeded")
	}

	return nil
}

func createTunnel(proto, listen, target string, tlsConfig *tls.Config) (*tunnel.Tunnel, error) {
	var p tunnel.Protocol

	switch proto {
	case "tcp":
		p = tcp.New()
	case "http2":
		if tlsConfig == nil {
			return nil, fmt.Errorf("TLS config required for HTTP/2")
		}
		p = http2.New(tlsConfig)
	case "http3":
		if tlsConfig == nil {
			return nil, fmt.Errorf("TLS config required for HTTP/3")
		}
		tlsConfig.NextProtos = []string{"h3"}
		p = http3.New(tlsConfig, nil)
	case "quic":
		if tlsConfig == nil {
			return nil, fmt.Errorf("TLS config required for QUIC")
		}
		if len(tlsConfig.NextProtos) == 0 {
			tlsConfig.NextProtos = []string{"quic-proxy"}
		}
		p = quic.New(tlsConfig, nil)
	default:
		return nil, fmt.Errorf("unknown protocol: %s", proto)
	}

	cfg := tunnel.Config{
		Protocol:   proto,
		ListenAddr: listen,
		TargetAddr: target,
		TLSConfig:  tlsConfig,
	}

	t, err := tunnel.New(cfg)
	if err != nil {
		return nil, err
	}

	t.SetProtocol(p)
	return t, nil
}

func loadTLSConfig(certFile, keyFile, caFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %w", err)
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		// Secure cipher suites
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		PreferServerCipherSuites: true,
	}

	if caFile != "" {
		caData, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file: %w", err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("failed to parse CA certificates")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.VerifyClientCertIfGiven
	}

	return cfg, nil
}
