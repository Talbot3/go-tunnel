package quic

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// TunnelRegistry manages tunnel registration and lifecycle.
// It provides thread-safe operations for tunnel storage and retrieval.
type TunnelRegistry struct {
	// Tunnel storage
	tunnels       sync.Map // tunnelID -> *Tunnel
	tunnelByDomain sync.Map // domain -> *Tunnel

	// Statistics
	activeTunnels atomic.Int64
	totalTunnels  atomic.Int64

	// Callbacks for tunnel lifecycle events
	onTunnelClose func(tunnel *Tunnel)

	// Port manager for port release
	portManager *PortManager

	// Stats handler for external notification
	statsHandler StatsHandler
}

// NewTunnelRegistry creates a new tunnel registry.
func NewTunnelRegistry(portManager *PortManager, statsHandler StatsHandler) *TunnelRegistry {
	return &TunnelRegistry{
		portManager:   portManager,
		statsHandler:  statsHandler,
	}
}

// Register adds a new tunnel to the registry.
func (r *TunnelRegistry) Register(tunnel *Tunnel) error {
	r.tunnels.Store(tunnel.ID, tunnel)
	r.activeTunnels.Add(1)
	r.totalTunnels.Add(1)

	if r.statsHandler != nil {
		r.statsHandler.AddActiveTunnel(1)
		r.statsHandler.AddTotalTunnel(1)
	}

	if tunnel.Domain != "" {
		r.tunnelByDomain.Store(tunnel.Domain, tunnel)
	}

	log.Printf("[TunnelRegistry] Tunnel registered: %s (domain=%s, protocol=%d)",
		tunnel.ID, tunnel.Domain, tunnel.Protocol)
	return nil
}

// Unregister removes a tunnel from the registry.
// This is the internal method that doesn't perform cleanup.
func (r *TunnelRegistry) Unregister(tunnelID string) {
	if v, ok := r.tunnels.LoadAndDelete(tunnelID); ok {
		tunnel := v.(*Tunnel)
		r.activeTunnels.Add(-1)

		if r.statsHandler != nil {
			r.statsHandler.AddActiveTunnel(-1)
		}

		if tunnel.Domain != "" {
			r.tunnelByDomain.Delete(tunnel.Domain)
		}

		// Release port
		if tunnel.Port > 0 && r.portManager != nil {
			r.portManager.Release(tunnel.Port)
		}

		log.Printf("[TunnelRegistry] Tunnel unregistered: %s", tunnelID)
	}
}

// Get retrieves a tunnel by ID.
// Returns nil if the tunnel doesn't exist.
func (r *TunnelRegistry) Get(tunnelID string) *Tunnel {
	if v, ok := r.tunnels.Load(tunnelID); ok {
		return v.(*Tunnel)
	}
	return nil
}

// GetByDomain retrieves a tunnel by domain.
// Returns nil if no tunnel is registered for the domain.
func (r *TunnelRegistry) GetByDomain(domain string) *Tunnel {
	if v, ok := r.tunnelByDomain.Load(domain); ok {
		return v.(*Tunnel)
	}
	return nil
}

// Delete removes a tunnel by ID and returns it.
// Returns nil if the tunnel doesn't exist.
func (r *TunnelRegistry) Delete(tunnelID string) *Tunnel {
	if v, ok := r.tunnels.LoadAndDelete(tunnelID); ok {
		tunnel := v.(*Tunnel)
		r.activeTunnels.Add(-1)

		if r.statsHandler != nil {
			r.statsHandler.AddActiveTunnel(-1)
		}

		if tunnel.Domain != "" {
			r.tunnelByDomain.Delete(tunnel.Domain)
		}

		// Release port
		if tunnel.Port > 0 && r.portManager != nil {
			r.portManager.Release(tunnel.Port)
		}

		return tunnel
	}
	return nil
}

// Range iterates over all tunnels.
// The iteration stops if fn returns false.
func (r *TunnelRegistry) Range(fn func(tunnel *Tunnel) bool) {
	r.tunnels.Range(func(key, value interface{}) bool {
		return fn(value.(*Tunnel))
	})
}

// Count returns the number of active tunnels.
func (r *TunnelRegistry) Count() int {
	count := 0
	r.tunnels.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// ActiveCount returns the atomic count of active tunnels.
func (r *TunnelRegistry) ActiveCount() int64 {
	return r.activeTunnels.Load()
}

// TotalCount returns the atomic count of total tunnels created.
func (r *TunnelRegistry) TotalCount() int64 {
	return r.totalTunnels.Load()
}

// SetOnTunnelClose sets the callback for tunnel close events.
func (r *TunnelRegistry) SetOnTunnelClose(fn func(tunnel *Tunnel)) {
	r.onTunnelClose = fn
}

// CloseTunnel closes a tunnel and removes it from the registry.
// This performs full cleanup including closing connections and listeners.
func (r *TunnelRegistry) CloseTunnel(tunnel *Tunnel, activeConns *atomic.Int64) {
	if tunnel.cancel != nil {
		tunnel.cancel()
	}

	// Close external listeners first to unblock Accept calls
	tunnel.externalListenerMu.Lock()
	if tunnel.externalListener != nil {
		tunnel.externalListener.Close()
		tunnel.externalListener = nil
	}
	if tunnel.externalQUICListener != nil {
		tunnel.externalQUICListener.Close()
		tunnel.externalQUICListener = nil
	}
	tunnel.externalListenerMu.Unlock()

	if tunnel.Conn != nil {
		tunnel.Conn.CloseWithError(0, "tunnel closed")
	}

	// Close external connections - use LoadAndDelete to avoid duplicate processing
	tunnel.ExternalConns.Range(func(key, value interface{}) bool {
		if conn, loaded := tunnel.ExternalConns.LoadAndDelete(key); loaded {
			conn.(interface{ Close() error }).Close()
			if activeConns != nil {
				activeConns.Add(-1)
			}
		}
		return true
	})

	// Remove from registry
	r.Delete(tunnel.ID)

	// Call callback if set
	if r.onTunnelClose != nil {
		r.onTunnelClose(tunnel)
	}

	log.Printf("[TunnelRegistry] Tunnel closed: %s", tunnel.ID)
}

// CheckTimeouts checks for timed-out tunnels and closes them.
// Returns the number of tunnels closed.
func (r *TunnelRegistry) CheckTimeouts(timeout time.Duration, activeConns *atomic.Int64) int {
	now := time.Now().Unix()
	closed := 0

	r.tunnels.Range(func(key, value interface{}) bool {
		tunnel := value.(*Tunnel)
		lastActive := tunnel.LastActive.Load()
		if now-lastActive > int64(timeout.Seconds()) {
			log.Printf("[TunnelRegistry] Tunnel %s timeout (inactive for %ds)", tunnel.ID, now-lastActive)
			r.CloseTunnel(tunnel, activeConns)
			closed++
		}
		return true
	})

	return closed
}

// Stats returns registry statistics.
type TunnelRegistryStats struct {
	ActiveTunnels int64
	TotalTunnels  int64
}

// GetStats returns current registry statistics.
func (r *TunnelRegistry) GetStats() TunnelRegistryStats {
	return TunnelRegistryStats{
		ActiveTunnels: r.activeTunnels.Load(),
		TotalTunnels:  r.totalTunnels.Load(),
	}
}
