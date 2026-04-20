package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/quic-go/quic-go"

	"github.com/Talbot3/go-tunnel/internal/limiter"
	"github.com/Talbot3/go-tunnel/internal/pool"
)

// ProtocolRouter routes external connections to appropriate handlers based on protocol.
// It manages external listeners for TCP, QUIC, and HTTP/3 protocols.
type ProtocolRouter struct {
	// Configuration
	tlsConfig  *tls.Config
	quicConfig *quic.Config
	bufferPool *pool.BufferPool
	connLimiter limiter.Limiter
	stats      StatsHandler
	portMgr    *PortManager

	// Wait group for goroutines
	wg *sync.WaitGroup

	// Context
	ctx context.Context
}

// ProtocolRouterConfig holds configuration for ProtocolRouter.
type ProtocolRouterConfig struct {
	// TLSConfig is the TLS configuration for secure protocols.
	TLSConfig *tls.Config

	// QUICConfig is the QUIC configuration.
	QUICConfig *quic.Config

	// BufferPool is the shared buffer pool.
	BufferPool *pool.BufferPool

	// ConnLimiter is the connection limiter.
	ConnLimiter limiter.Limiter

	// StatsHandler is the statistics collector.
	StatsHandler StatsHandler

	// PortManager is the port allocator.
	PortManager *PortManager
}

// NewProtocolRouter creates a new protocol router.
func NewProtocolRouter(config ProtocolRouterConfig) *ProtocolRouter {
	return &ProtocolRouter{
		tlsConfig:   config.TLSConfig,
		quicConfig:  config.QUICConfig,
		bufferPool:  config.BufferPool,
		connLimiter: config.ConnLimiter,
		stats:       config.StatsHandler,
		portMgr:     config.PortManager,
	}
}

// SetContext sets the context and wait group for the router.
func (r *ProtocolRouter) SetContext(ctx context.Context, wg *sync.WaitGroup) {
	r.ctx = ctx
	r.wg = wg
}

// StartExternalListener starts an external listener based on tunnel protocol.
// Returns the listener address and any error.
func (r *ProtocolRouter) StartExternalListener(tunnel *Tunnel, onConnection func(conn interface{})) error {
	switch tunnel.Protocol {
	case ProtocolTCP, ProtocolHTTP:
		return r.startTCPListener(tunnel, onConnection)
	case ProtocolQUIC:
		return r.startQUICListener(tunnel, onConnection)
	case ProtocolHTTP3:
		return r.startHTTP3Listener(tunnel, onConnection)
	default:
		return fmt.Errorf("unsupported protocol: %d", tunnel.Protocol)
	}
}

// startTCPListener starts a TCP listener for TCP/HTTP tunnels.
func (r *ProtocolRouter) startTCPListener(tunnel *Tunnel, onConnection func(conn interface{})) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", tunnel.Port))
	if err != nil {
		return fmt.Errorf("listen port %d failed: %w", tunnel.Port, err)
	}

	// Store listener
	tunnel.externalListenerMu.Lock()
	tunnel.externalListener = listener
	tunnel.externalListenerMu.Unlock()

	log.Printf("[ProtocolRouter] TCP listener started for %s on :%d", tunnel.ID, tunnel.Port)

	// Accept loop
	go func() {
		defer listener.Close()
		defer func() {
			tunnel.externalListenerMu.Lock()
			tunnel.externalListener = nil
			tunnel.externalListenerMu.Unlock()
		}()

		for {
			select {
			case <-tunnel.ctx.Done():
				return
			default:
			}

			extConn, err := listener.Accept()
			if err != nil {
				if tunnel.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}

			if r.wg != nil {
				r.wg.Add(1)
			}
			go func() {
				if r.wg != nil {
					defer r.wg.Done()
				}
				onConnection(extConn)
			}()
		}
	}()

	return nil
}

// startQUICListener starts a QUIC listener for QUIC tunnels.
func (r *ProtocolRouter) startQUICListener(tunnel *Tunnel, onConnection func(conn interface{})) error {
	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", tunnel.Port))
	if err != nil {
		return fmt.Errorf("resolve UDP address failed: %w", err)
	}

	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen UDP port %d failed: %w", tunnel.Port, err)
	}

	// Create QUIC listener
	externalTLSConfig := r.tlsConfig
	if externalTLSConfig == nil {
		udpConn.Close()
		return errors.New("no TLS config for external QUIC listener")
	}
	externalTLSConfig = externalTLSConfig.Clone()

	externalListener, err := quic.Listen(udpConn, externalTLSConfig, r.quicConfig)
	if err != nil {
		udpConn.Close()
		return fmt.Errorf("listen external QUIC failed: %w", err)
	}

	// Store listener
	tunnel.externalListenerMu.Lock()
	tunnel.externalQUICListener = externalListener
	tunnel.externalListenerMu.Unlock()

	log.Printf("[ProtocolRouter] QUIC listener started for %s on :%d", tunnel.ID, tunnel.Port)

	// Accept loop
	go func() {
		defer externalListener.Close()
		defer func() {
			tunnel.externalListenerMu.Lock()
			tunnel.externalQUICListener = nil
			tunnel.externalListenerMu.Unlock()
		}()

		for {
			select {
			case <-tunnel.ctx.Done():
				return
			default:
			}

			extConn, err := externalListener.Accept(r.ctx)
			if err != nil {
				if r.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}

			if r.wg != nil {
				r.wg.Add(1)
			}
			go func() {
				if r.wg != nil {
					defer r.wg.Done()
				}
				onConnection(extConn)
			}()
		}
	}()

	return nil
}

// startHTTP3Listener starts an HTTP/3 listener for HTTP/3 tunnels.
func (r *ProtocolRouter) startHTTP3Listener(tunnel *Tunnel, onConnection func(conn interface{})) error {
	// HTTP/3 uses QUIC as transport, similar to QUIC listener
	// but with HTTP/3 semantics (ALPN "h3")
	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", tunnel.Port))
	if err != nil {
		return fmt.Errorf("resolve UDP address failed: %w", err)
	}

	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen UDP port %d failed: %w", tunnel.Port, err)
	}

	// Create QUIC listener with HTTP/3 ALPN
	externalTLSConfig := r.tlsConfig
	if externalTLSConfig == nil {
		udpConn.Close()
		return errors.New("no TLS config for external HTTP/3 listener")
	}
	externalTLSConfig = externalTLSConfig.Clone()
	externalTLSConfig.NextProtos = []string{"h3"}

	externalListener, err := quic.Listen(udpConn, externalTLSConfig, r.quicConfig)
	if err != nil {
		udpConn.Close()
		return fmt.Errorf("listen external HTTP/3 failed: %w", err)
	}

	// Store listener
	tunnel.externalListenerMu.Lock()
	tunnel.externalQUICListener = externalListener
	tunnel.externalListenerMu.Unlock()

	log.Printf("[ProtocolRouter] HTTP/3 listener started for %s on :%d", tunnel.ID, tunnel.Port)

	// Accept loop
	go func() {
		defer externalListener.Close()
		defer func() {
			tunnel.externalListenerMu.Lock()
			tunnel.externalQUICListener = nil
			tunnel.externalListenerMu.Unlock()
		}()

		for {
			select {
			case <-tunnel.ctx.Done():
				return
			default:
			}

			extConn, err := externalListener.Accept(r.ctx)
			if err != nil {
				if r.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}

			if r.wg != nil {
				r.wg.Add(1)
			}
			go func() {
				if r.wg != nil {
					defer r.wg.Done()
				}
				onConnection(extConn)
			}()
		}
	}()

	return nil
}

// StopExternalListener stops the external listener for a tunnel.
func (r *ProtocolRouter) StopExternalListener(tunnel *Tunnel) error {
	tunnel.externalListenerMu.Lock()
	defer tunnel.externalListenerMu.Unlock()

	var errs []error

	if tunnel.externalListener != nil {
		if err := tunnel.externalListener.Close(); err != nil {
			errs = append(errs, err)
		}
		tunnel.externalListener = nil
	}

	if tunnel.externalQUICListener != nil {
		if err := tunnel.externalQUICListener.Close(); err != nil {
			errs = append(errs, err)
		}
		tunnel.externalQUICListener = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping listeners: %v", errs)
	}
	return nil
}

// AllocatePort allocates a port for a tunnel.
func (r *ProtocolRouter) AllocatePort() (int, error) {
	if r.portMgr == nil {
		return 0, errors.New("no port manager configured")
	}
	return r.portMgr.Allocate()
}

// ReleasePort releases a port.
func (r *ProtocolRouter) ReleasePort(port int) {
	if r.portMgr != nil {
		r.portMgr.Release(port)
	}
}
