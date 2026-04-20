package quic

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/Talbot3/go-tunnel/internal/retry"
)

// ConnectionManager manages QUIC connection lifecycle for MuxClient.
// It handles connection establishment, 0-RTT reconnection, and automatic reconnection.
type ConnectionManager struct {
	// Configuration
	config MuxClientConfig

	// Connection state
	conn      quic.Connection
	connMu    sync.RWMutex
	connected atomic.Bool

	// Reconnection
	reconnectCh chan struct{}
	retrier     *retry.Retrier

	// Context
	ctx    context.Context
	cancel context.CancelFunc

	// Callbacks
	onConnected    func()
	onDisconnected func(err error)
}

// ConnectionManagerConfig holds configuration for ConnectionManager.
type ConnectionManagerConfig struct {
	// ServerAddr is the server address to connect to.
	ServerAddr string

	// TLSConfig is the TLS configuration.
	TLSConfig *tls.Config

	// QUICConfig is the QUIC configuration.
	QUICConfig *quic.Config

	// Enable0RTT enables 0-RTT fast reconnection.
	Enable0RTT bool

	// SessionCache is the TLS session cache for 0-RTT.
	SessionCache tls.ClientSessionCache

	// MaxReconnectTries is the maximum number of reconnection attempts.
	MaxReconnectTries int

	// ReconnectInterval is the initial reconnection interval.
	ReconnectInterval time.Duration

	// Protocol is the tunnel protocol.
	Protocol byte
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager(config ConnectionManagerConfig) *ConnectionManager {
	cm := &ConnectionManager{
		reconnectCh: make(chan struct{}, 1),
	}

	// Create retrier with exponential backoff
	cm.retrier = retry.NewRetrier(retry.Config{
		MaxAttempts:  config.MaxReconnectTries,
		InitialDelay: config.ReconnectInterval,
		MaxDelay:     5 * time.Minute,
		Multiplier:   2.0,
		Jitter:       0.1,
		RetryableError: func(err error) bool {
			// All connection errors are retryable
			return true
		},
	})

	return cm
}

// Connect establishes a standard QUIC connection.
func (cm *ConnectionManager) Connect(ctx context.Context, config ConnectionManagerConfig) (quic.Connection, error) {
	log.Printf("[ConnectionManager] Connecting to %s...", config.ServerAddr)

	// 1. Resolve address
	udpAddr, err := net.ResolveUDPAddr("udp", config.ServerAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve address failed: %w", err)
	}

	// 2. Create UDP connection
	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, fmt.Errorf("listen UDP failed: %w", err)
	}

	// 3. Configure TLS
	tlsConf := config.TLSConfig
	if tlsConf == nil {
		tlsConf = &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"quic-tunnel"},
		}
	} else {
		tlsConf = tlsConf.Clone()
	}
	if len(tlsConf.NextProtos) == 0 {
		tlsConf.NextProtos = []string{"quic-tunnel"}
	}

	// Enable TLS session cache for 0-RTT
	if config.Enable0RTT && config.SessionCache != nil {
		tlsConf.ClientSessionCache = config.SessionCache
	}

	// 4. Dial QUIC
	conn, err := quic.Dial(ctx, udpConn, udpAddr, tlsConf, config.QUICConfig)
	if err != nil {
		udpConn.Close()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			return nil, fmt.Errorf("dial QUIC failed: %w", err)
		}
	}

	log.Printf("[ConnectionManager] QUIC connection established")
	return conn, nil
}

// Connect0RTT establishes a 0-RTT QUIC connection for fast reconnection.
func (cm *ConnectionManager) Connect0RTT(ctx context.Context, config ConnectionManagerConfig) (quic.Connection, error) {
	log.Printf("[ConnectionManager] Attempting 0-RTT connection to %s...", config.ServerAddr)

	// 1. Resolve address
	udpAddr, err := net.ResolveUDPAddr("udp", config.ServerAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve address failed: %w", err)
	}

	// 2. Create UDP connection
	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, fmt.Errorf("listen UDP failed: %w", err)
	}

	// 3. Configure TLS for 0-RTT
	tlsConf := config.TLSConfig
	if tlsConf == nil {
		tlsConf = &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"quic-tunnel"},
		}
	} else {
		tlsConf = tlsConf.Clone()
	}
	if len(tlsConf.NextProtos) == 0 {
		tlsConf.NextProtos = []string{"quic-tunnel"}
	}

	// Session cache must be set for 0-RTT
	if config.SessionCache != nil {
		tlsConf.ClientSessionCache = config.SessionCache
	}

	// 4. Use DialEarly for 0-RTT support
	conn, err := quic.DialEarly(ctx, udpConn, udpAddr, tlsConf, config.QUICConfig)
	if err != nil {
		udpConn.Close()
		return nil, fmt.Errorf("dial QUIC early failed: %w", err)
	}

	log.Printf("[ConnectionManager] QUIC 0-RTT connection established")
	return conn, nil
}

// GetConnection returns the current connection.
// Thread-safe.
func (cm *ConnectionManager) GetConnection() quic.Connection {
	cm.connMu.RLock()
	defer cm.connMu.RUnlock()
	return cm.conn
}

// SetConnection sets the current connection.
// Thread-safe.
func (cm *ConnectionManager) SetConnection(conn quic.Connection) {
	cm.connMu.Lock()
	defer cm.connMu.Unlock()
	cm.conn = conn
}

// IsConnected returns whether the manager has an active connection.
func (cm *ConnectionManager) IsConnected() bool {
	return cm.connected.Load()
}

// SetConnected sets the connected state.
func (cm *ConnectionManager) SetConnected(connected bool) {
	cm.connected.Store(connected)
}

// TriggerReconnect signals that a reconnection is needed.
// Non-blocking.
func (cm *ConnectionManager) TriggerReconnect() {
	select {
	case cm.reconnectCh <- struct{}{}:
	default:
		// Already signaled
	}
}

// ReconnectChannel returns the channel for reconnection signals.
func (cm *ConnectionManager) ReconnectChannel() <-chan struct{} {
	return cm.reconnectCh
}

// Close closes the current connection.
func (cm *ConnectionManager) Close() {
	cm.connMu.Lock()
	defer cm.connMu.Unlock()

	if cm.conn != nil {
		cm.conn.CloseWithError(0, "connection manager closed")
		cm.conn = nil
	}
	cm.connected.Store(false)
}

// RunConnectLoop runs the main connection loop with automatic reconnection.
// This is typically run in a goroutine.
func (cm *ConnectionManager) RunConnectLoop(
	ctx context.Context,
	config ConnectionManagerConfig,
	hasCachedState func() bool,
	onConnect func(conn quic.Connection) error,
) error {
	cm.ctx, cm.cancel = context.WithCancel(ctx)
	defer cm.cancel()

	reconnectTries := 0

	for {
		select {
		case <-cm.ctx.Done():
			return cm.ctx.Err()
		default:
		}

		var err error

		// Try 0-RTT connection first if enabled and we have cached state
		if config.Enable0RTT && config.SessionCache != nil && hasCachedState() {
			err = cm.retrier.Do(cm.ctx, func() error {
				conn, connErr := cm.Connect0RTT(cm.ctx, config)
				if connErr != nil {
					return connErr
				}
				cm.SetConnection(conn)
				return onConnect(conn)
			})
			if err != nil {
				// Fall back to regular connection
				log.Printf("[ConnectionManager] 0-RTT connection failed, falling back to regular: %v", err)
				err = cm.retrier.Do(cm.ctx, func() error {
					conn, connErr := cm.Connect(cm.ctx, config)
					if connErr != nil {
						return connErr
					}
					cm.SetConnection(conn)
					return onConnect(conn)
				})
			}
		} else {
			err = cm.retrier.Do(cm.ctx, func() error {
				conn, connErr := cm.Connect(cm.ctx, config)
				if connErr != nil {
					return connErr
				}
				cm.SetConnection(conn)
				return onConnect(conn)
			})
		}

		if err != nil {
			if cm.ctx.Err() != nil {
				return cm.ctx.Err() // Context canceled
			}
			log.Printf("[ConnectionManager] Connect failed after retries: %v", err)

			if config.MaxReconnectTries > 0 && reconnectTries >= config.MaxReconnectTries {
				log.Printf("[ConnectionManager] Max reconnect tries reached")
				return fmt.Errorf("max reconnect tries reached")
			}
			reconnectTries++
			continue
		}

		reconnectTries = 0
		cm.connected.Store(true)

		if cm.onConnected != nil {
			cm.onConnected()
		}

		// Wait for reconnection signal or context done
		select {
		case <-cm.ctx.Done():
			return cm.ctx.Err()
		case <-cm.reconnectCh:
			log.Printf("[ConnectionManager] Reconnect requested")
			cm.connected.Store(false)
		}
	}
}

// SetOnConnected sets the callback for successful connection.
func (cm *ConnectionManager) SetOnConnected(fn func()) {
	cm.onConnected = fn
}

// SetOnDisconnected sets the callback for disconnection.
func (cm *ConnectionManager) SetOnDisconnected(fn func(err error)) {
	cm.onDisconnected = fn
}

// SupportsDatagrams checks if the current connection supports datagrams.
func (cm *ConnectionManager) SupportsDatagrams() bool {
	cm.connMu.RLock()
	defer cm.connMu.RUnlock()
	if cm.conn == nil {
		return false
	}
	return cm.conn.ConnectionState().SupportsDatagrams
}
