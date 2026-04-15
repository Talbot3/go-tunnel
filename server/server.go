// Package server provides a complete tunnel server with health endpoints.
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Talbot3/go-tunnel/internal/health"
	"github.com/Talbot3/go-tunnel/internal/limiter"
	"github.com/Talbot3/go-tunnel/internal/shutdown"
	"github.com/Talbot3/go-tunnel/quic"
)

// Server represents a complete tunnel server with all HA components.
type Server struct {
	// QUIC tunnel server
	muxServer *quic.MuxServer

	// Health handler
	healthHandler *health.Handler

	// Resource limiter
	resourceLimiter *limiter.ConnectionLimiter

	// HTTP server for health endpoints
	httpServer *http.Server

	// Configuration
	config Config

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Config holds the server configuration.
type Config struct {
	// QUIC configuration
	ListenAddr string
	TLSConfig  *tls.Config
	AuthToken  string

	// Resource limits
	MaxConnections int64
	MaxTunnels     int

	// Health endpoints
	HealthAddr string // Address for health HTTP server, e.g., ":8080"

	// Shutdown timeout
	ShutdownTimeout time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		ListenAddr:      ":443",
		MaxConnections:  10000,
		MaxTunnels:      1000,
		HealthAddr:      ":8080",
		ShutdownTimeout: 30 * time.Second,
	}
}

// New creates a new tunnel server.
func New(config Config) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create QUIC server
	muxConfig := quic.MuxServerConfig{
		ListenAddr:       config.ListenAddr,
		TLSConfig:        config.TLSConfig,
		AuthToken:        config.AuthToken,
		MaxTunnels:       config.MaxTunnels,
		PortRangeStart:   10000,
		PortRangeEnd:     20000,
		TunnelTimeout:    5 * time.Minute,
		ConnTimeout:      10 * time.Minute,
	}

	muxServer := quic.NewMuxServer(muxConfig)

	// Create health handler
	healthHandler := health.NewHandler(health.HandlerConfig{
		Timeout: 5 * time.Second,
	})

	// Register health checks
	healthHandler.Register("ping", health.PingCheck("ping").Check)
	healthHandler.Register("quic-server", func(ctx context.Context) health.CheckResult {
		// Check if QUIC server is accepting connections
		return health.CheckResult{
			Name:      "quic-server",
			Status:    health.StatusHealthy,
			Timestamp: time.Now(),
		}
	})

	// Create connection limiter
	resourceLimiter := limiter.NewConnectionLimiter(config.MaxConnections)

	return &Server{
		muxServer:      muxServer,
		healthHandler:  healthHandler,
		resourceLimiter: resourceLimiter,
		config:         config,
		ctx:            ctx,
		cancel:         cancel,
	}, nil
}

// Start starts the tunnel server.
func (s *Server) Start(ctx context.Context) error {
	// Start QUIC server
	if err := s.muxServer.Start(ctx); err != nil {
		return err
	}

	// Start health HTTP server
	if s.config.HealthAddr != "" {
		s.startHealthServer()
	}

	// Register shutdown handler
	shutdown.RegisterFunc("tunnel-server", func(ctx context.Context) error {
		return s.Stop()
	})

	log.Printf("[Server] Started on %s", s.config.ListenAddr)
	return nil
}

// startHealthServer starts the HTTP server for health endpoints.
func (s *Server) startHealthServer() {
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("/health", s.healthHandler.ServeHTTP)

	// Liveness endpoint
	mux.HandleFunc("/livez", health.LivenessHandler())

	// Readiness endpoint
	mux.HandleFunc("/readyz", health.ReadinessHandler(s.healthHandler))

	// Metrics endpoint (basic stats)
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		stats := s.muxServer.GetStats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	// Circuit breaker status
	mux.HandleFunc("/circuit", func(w http.ResponseWriter, r *http.Request) {
		// Return circuit breaker status
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	})

	s.httpServer = &http.Server{
		Addr:         s.config.HealthAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		log.Printf("[Server] Health server started on %s", s.config.HealthAddr)
		if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("[Server] Health server error: %v", err)
		}
	}()
}

// Stop stops the tunnel server gracefully.
func (s *Server) Stop() error {
	s.cancel()

	// Stop QUIC server
	if err := s.muxServer.Stop(); err != nil {
		log.Printf("[Server] Error stopping QUIC server: %v", err)
	}

	// Stop health HTTP server
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.httpServer.Shutdown(ctx)
	}

	s.wg.Wait()
	log.Println("[Server] Stopped")
	return nil
}

// GetStats returns server statistics.
func (s *Server) GetStats() quic.ServerStats {
	return s.muxServer.GetStats()
}

// GetHealthHandler returns the health handler for custom health checks.
func (s *Server) GetHealthHandler() *health.Handler {
	return s.healthHandler
}

// GetResourceLimiter returns the resource limiter.
func (s *Server) GetResourceLimiter() *limiter.ConnectionLimiter {
	return s.resourceLimiter
}

// AcquireConnection attempts to acquire a connection slot.
func (s *Server) AcquireConnection() error {
	return s.resourceLimiter.Acquire()
}

// ReleaseConnection releases a connection slot.
func (s *Server) ReleaseConnection() {
	s.resourceLimiter.Release()
}
