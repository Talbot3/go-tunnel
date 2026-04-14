package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/wld/go-tunnel/config"
	"github.com/wld/go-tunnel/forward"
	"github.com/wld/go-tunnel/protocol"
	"github.com/wld/go-tunnel/protocol/http2"
	"github.com/wld/go-tunnel/protocol/http3"
	pquic "github.com/wld/go-tunnel/protocol/quic"
	"github.com/wld/go-tunnel/protocol/tcp"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	// Command line flags
	cfgPath := flag.String("config", "", "Configuration file path")
	listen := flag.String("listen", "", "Listen address (overrides config)")
	target := flag.String("target", "", "Target address (overrides config)")
	proto := flag.String("protocol", "tcp", "Protocol: tcp, http2, http3, quic")
	showVersion := flag.Bool("version", false, "Show version info")
	flag.Parse()

	if *showVersion {
		fmt.Printf("go-tunnel %s (built %s)\n", version, buildTime)
		fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Load configuration
	cfgManager := config.NewManager()
	if *cfgPath != "" {
		if err := cfgManager.Load(*cfgPath); err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
	} else {
		cfgManager = &config.Manager{}
		_ = cfgManager // Use defaults
	}

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		for sig := range sigChan {
			switch sig {
			case syscall.SIGHUP:
				log.Println("Received SIGHUP, reloading config...")
				if err := cfgManager.Reload(); err != nil {
					log.Printf("Failed to reload config: %v", err)
				}
			default:
				log.Printf("Received %v, shutting down...", sig)
				cancel()
			}
		}
	}()

	// Create protocol registry
	registry := protocol.NewRegistry()

	// Generate TLS config for HTTP/2, HTTP/3, QUIC
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // For demo; use proper certs in production
	}

	// Register protocols
	registry.Register(tcp.New())
	registry.Register(http2.New(tlsConfig))
	registry.Register(pquic.New(tlsConfig, nil))
	registry.Register(http3.New(tlsConfig, nil))

	// Get the protocol handler
	p, ok := registry.Get(*proto)
	if !ok {
		log.Fatalf("Unknown protocol: %s", *proto)
	}

	// Determine listen and target addresses
	cfg := cfgManager.Get()
	listenAddr := *listen
	targetAddr := *target

	if listenAddr == "" {
		if cfg != nil && len(cfg.Protocols) > 0 {
			for _, pc := range cfg.Protocols {
				if pc.Name == *proto {
					listenAddr = pc.Listen
					targetAddr = pc.Target
					break
				}
			}
		}
		if listenAddr == "" {
			listenAddr = ":8080"
		}
	}

	if targetAddr == "" {
		targetAddr = "127.0.0.1:80"
	}

	log.Printf("Starting %s forwarder: %s -> %s", p.Name(), listenAddr, targetAddr)
	log.Printf("Platform: %s/%s", runtime.GOOS, runtime.GOARCH)

	// Start listening
	listener, err := p.Listen(listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	log.Printf("Listening on %s", listener.Addr())

	// Accept connections
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("Accept error: %v", err)
					continue
				}
			}

			go handleConnection(ctx, p, conn, targetAddr)
		}
	}()

	// Wait for shutdown
	<-ctx.Done()
	log.Println("Shutting down...")
}

func handleConnection(ctx context.Context, p protocol.Protocol, src net.Conn, targetAddr string) {
	defer src.Close()

	// Connect to target
	dst, err := p.Dial(targetAddr)
	if err != nil {
		log.Printf("Failed to dial %s: %v", targetAddr, err)
		return
	}
	defer dst.Close()

	log.Printf("Connection: %s <-> %s", src.RemoteAddr(), dst.RemoteAddr())

	// Start bidirectional forwarding
	fwd := forward.NewForwarder()

	done := make(chan struct{}, 2)

	// Forward src -> dst
	go func() {
		defer func() { done <- struct{}{} }()
		if err := fwd.Forward(src, dst); err != nil {
			if !forward.IsClosedErr(err) {
				log.Printf("Forward error (src->dst): %v", err)
			}
		}
	}()

	// Forward dst -> src
	go func() {
		defer func() { done <- struct{}{} }()
		if err := fwd.Forward(dst, src); err != nil {
			if !forward.IsClosedErr(err) {
				log.Printf("Forward error (dst->src): %v", err)
			}
		}
	}()

	// Wait for one direction to finish
	select {
	case <-ctx.Done():
	case <-done:
	}

	log.Printf("Connection closed: %s", src.RemoteAddr())
}
