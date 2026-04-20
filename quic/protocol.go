package quic

import (
	"context"
	"crypto/tls"
	"net"
	"sync"

	"github.com/quic-go/quic-go"

	"github.com/Talbot3/go-tunnel/internal/limiter"
	"github.com/Talbot3/go-tunnel/internal/pool"
)

// ProtocolHandler defines the interface for protocol-specific operations.
// Each protocol (TCP, QUIC, HTTP/3) has its own implementation.
// This follows the Strategy pattern, allowing protocols to be added
// without modifying existing code (Open-Closed Principle).
type ProtocolHandler interface {
	// Protocol returns the protocol type byte.
	Protocol() byte

	// StartExternalListener starts the external listener for this protocol.
	// Returns the listener address and any error.
	StartExternalListener(ctx context.Context, tunnel *Tunnel, port int) (net.Addr, error)

	// StopExternalListener stops the external listener.
	StopExternalListener(tunnel *Tunnel) error

	// HandleExternalConnection handles an incoming external connection.
	// The connection should be forwarded through the tunnel.
	// conn is either net.Conn or quic.Connection depending on protocol.
	HandleExternalConnection(ctx context.Context, tunnel *Tunnel, conn interface{}) error

	// HandleDataStream handles an incoming data stream from the tunnel.
	HandleDataStream(ctx context.Context, tunnel *Tunnel, stream quic.Stream, header *SessionHeader) error
}

// ProtocolHandlerConfig holds common configuration for protocol handlers.
type ProtocolHandlerConfig struct {
	// TLSConfig is the TLS configuration for secure protocols.
	TLSConfig *tls.Config

	// QUICConfig is the QUIC configuration for QUIC-based protocols.
	QUICConfig *quic.Config

	// BufferPool is the shared buffer pool for data operations.
	BufferPool *pool.BufferPool

	// ConnLimiter is the connection limiter for resource protection.
	ConnLimiter limiter.Limiter

	// StatsHandler is the statistics collector.
	StatsHandler StatsHandler

	// PortManager is the port allocator for TCP tunnels.
	PortManager *PortManager
}

// ProtocolHandlerRegistry manages protocol handlers.
// It allows dynamic registration and lookup of handlers by protocol type.
type ProtocolHandlerRegistry struct {
	handlers sync.Map // protocol byte -> ProtocolHandler
	config   ProtocolHandlerConfig
}

// NewProtocolHandlerRegistry creates a new protocol handler registry.
func NewProtocolHandlerRegistry(config ProtocolHandlerConfig) *ProtocolHandlerRegistry {
	registry := &ProtocolHandlerRegistry{
		config: config,
	}
	// Register default handlers
	registry.Register(ProtocolTCP, NewTCPProtocolHandler(config))
	registry.Register(ProtocolQUIC, NewQUICProtocolHandler(config))
	registry.Register(ProtocolHTTP3, NewHTTP3ProtocolHandler(config))
	return registry
}

// Register adds a protocol handler to the registry.
func (r *ProtocolHandlerRegistry) Register(protocol byte, handler ProtocolHandler) {
	r.handlers.Store(protocol, handler)
}

// Get retrieves a protocol handler by protocol type.
// Returns nil if no handler is registered for the protocol.
func (r *ProtocolHandlerRegistry) Get(protocol byte) ProtocolHandler {
	if v, ok := r.handlers.Load(protocol); ok {
		return v.(ProtocolHandler)
	}
	return nil
}

// Range iterates over all registered protocol handlers.
func (r *ProtocolHandlerRegistry) Range(fn func(protocol byte, handler ProtocolHandler) bool) {
	r.handlers.Range(func(key, value interface{}) bool {
		return fn(key.(byte), value.(ProtocolHandler))
	})
}

// ============================================
// TCP Protocol Handler
// ============================================

// TCPProtocolHandler handles TCP protocol tunnels.
type TCPProtocolHandler struct {
	config ProtocolHandlerConfig
}

// NewTCPProtocolHandler creates a new TCP protocol handler.
func NewTCPProtocolHandler(config ProtocolHandlerConfig) *TCPProtocolHandler {
	return &TCPProtocolHandler{config: config}
}

// Protocol returns the TCP protocol type.
func (h *TCPProtocolHandler) Protocol() byte {
	return ProtocolTCP
}

// StartExternalListener starts a TCP listener for external connections.
func (h *TCPProtocolHandler) StartExternalListener(ctx context.Context, tunnel *Tunnel, port int) (net.Addr, error) {
	// Implementation will be moved from quic.go
	return nil, nil
}

// StopExternalListener stops the TCP listener.
func (h *TCPProtocolHandler) StopExternalListener(tunnel *Tunnel) error {
	tunnel.externalListenerMu.Lock()
	defer tunnel.externalListenerMu.Unlock()

	if tunnel.externalListener != nil {
		err := tunnel.externalListener.Close()
		tunnel.externalListener = nil
		return err
	}
	return nil
}

// HandleExternalConnection handles an incoming TCP connection.
func (h *TCPProtocolHandler) HandleExternalConnection(ctx context.Context, tunnel *Tunnel, conn interface{}) error {
	// Implementation will be moved from quic.go
	return nil
}

// HandleDataStream handles an incoming data stream for TCP tunnel.
func (h *TCPProtocolHandler) HandleDataStream(ctx context.Context, tunnel *Tunnel, stream quic.Stream, header *SessionHeader) error {
	// Implementation will be moved from quic.go
	return nil
}

// ============================================
// QUIC Protocol Handler
// ============================================

// QUICProtocolHandler handles QUIC protocol tunnels.
type QUICProtocolHandler struct {
	config ProtocolHandlerConfig
}

// NewQUICProtocolHandler creates a new QUIC protocol handler.
func NewQUICProtocolHandler(config ProtocolHandlerConfig) *QUICProtocolHandler {
	return &QUICProtocolHandler{config: config}
}

// Protocol returns the QUIC protocol type.
func (h *QUICProtocolHandler) Protocol() byte {
	return ProtocolQUIC
}

// StartExternalListener starts a QUIC listener for external connections.
func (h *QUICProtocolHandler) StartExternalListener(ctx context.Context, tunnel *Tunnel, port int) (net.Addr, error) {
	// Implementation will be moved from quic.go
	return nil, nil
}

// StopExternalListener stops the QUIC listener.
func (h *QUICProtocolHandler) StopExternalListener(tunnel *Tunnel) error {
	tunnel.externalListenerMu.Lock()
	defer tunnel.externalListenerMu.Unlock()

	if tunnel.externalQUICListener != nil {
		err := tunnel.externalQUICListener.Close()
		tunnel.externalQUICListener = nil
		return err
	}
	return nil
}

// HandleExternalConnection handles an incoming QUIC connection.
func (h *QUICProtocolHandler) HandleExternalConnection(ctx context.Context, tunnel *Tunnel, conn interface{}) error {
	// Implementation will be moved from quic.go
	return nil
}

// HandleDataStream handles an incoming data stream for QUIC tunnel.
func (h *QUICProtocolHandler) HandleDataStream(ctx context.Context, tunnel *Tunnel, stream quic.Stream, header *SessionHeader) error {
	// Implementation will be moved from quic.go
	return nil
}

// ============================================
// HTTP/3 Protocol Handler
// ============================================

// HTTP3ProtocolHandler handles HTTP/3 protocol tunnels.
type HTTP3ProtocolHandler struct {
	config ProtocolHandlerConfig
}

// NewHTTP3ProtocolHandler creates a new HTTP/3 protocol handler.
func NewHTTP3ProtocolHandler(config ProtocolHandlerConfig) *HTTP3ProtocolHandler {
	return &HTTP3ProtocolHandler{config: config}
}

// Protocol returns the HTTP/3 protocol type.
func (h *HTTP3ProtocolHandler) Protocol() byte {
	return ProtocolHTTP3
}

// StartExternalListener starts an HTTP/3 listener for external connections.
func (h *HTTP3ProtocolHandler) StartExternalListener(ctx context.Context, tunnel *Tunnel, port int) (net.Addr, error) {
	// Implementation will be moved from quic.go
	return nil, nil
}

// StopExternalListener stops the HTTP/3 listener.
func (h *HTTP3ProtocolHandler) StopExternalListener(tunnel *Tunnel) error {
	// HTTP/3 uses the same QUIC listener as QUIC protocol
	return nil
}

// HandleExternalConnection handles an incoming HTTP/3 connection.
func (h *HTTP3ProtocolHandler) HandleExternalConnection(ctx context.Context, tunnel *Tunnel, conn interface{}) error {
	// Implementation will be moved from quic.go
	return nil
}

// HandleDataStream handles an incoming data stream for HTTP/3 tunnel.
func (h *HTTP3ProtocolHandler) HandleDataStream(ctx context.Context, tunnel *Tunnel, stream quic.Stream, header *SessionHeader) error {
	// Implementation will be moved from quic.go
	return nil
}

// Compile-time interface verification
var (
	_ ProtocolHandler = (*TCPProtocolHandler)(nil)
	_ ProtocolHandler = (*QUICProtocolHandler)(nil)
	_ ProtocolHandler = (*HTTP3ProtocolHandler)(nil)
)
