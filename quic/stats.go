package quic

import (
	"sync/atomic"
)

// StatsHandler defines the interface for collecting statistics.
// This allows different implementations to be injected for monitoring.
type StatsHandler interface {
	// AddActiveConnection adds delta to active connection count.
	AddActiveConnection(delta int64)

	// AddTotalConnection adds delta to total connection count.
	AddTotalConnection(delta int64)

	// AddBytesIn adds n to bytes received count.
	AddBytesIn(n int64)

	// AddBytesOut adds n to bytes sent count.
	AddBytesOut(n int64)

	// AddActiveTunnel adds delta to active tunnel count.
	AddActiveTunnel(delta int64)

	// AddTotalTunnel adds delta to total tunnel count.
	AddTotalTunnel(delta int64)
}

// ServerStatsHandler implements StatsHandler for MuxServer.
// It wraps atomic counters for thread-safe statistics collection.
type ServerStatsHandler struct {
	activeConns   *atomic.Int64
	totalConns    *atomic.Int64
	bytesIn       *atomic.Int64
	bytesOut      *atomic.Int64
	activeTunnels *atomic.Int64
	totalTunnels  *atomic.Int64
}

// NewServerStatsHandler creates a new server stats handler.
func NewServerStatsHandler(
	activeConns, totalConns, bytesIn, bytesOut, activeTunnels, totalTunnels *atomic.Int64,
) *ServerStatsHandler {
	return &ServerStatsHandler{
		activeConns:   activeConns,
		totalConns:    totalConns,
		bytesIn:       bytesIn,
		bytesOut:      bytesOut,
		activeTunnels: activeTunnels,
		totalTunnels:  totalTunnels,
	}
}

// AddActiveConnection adds delta to active connection count.
func (h *ServerStatsHandler) AddActiveConnection(delta int64) {
	if h.activeConns != nil {
		h.activeConns.Add(delta)
	}
}

// AddTotalConnection adds delta to total connection count.
func (h *ServerStatsHandler) AddTotalConnection(delta int64) {
	if h.totalConns != nil {
		h.totalConns.Add(delta)
	}
}

// AddBytesIn adds n to bytes received count.
func (h *ServerStatsHandler) AddBytesIn(n int64) {
	if h.bytesIn != nil {
		h.bytesIn.Add(n)
	}
}

// AddBytesOut adds n to bytes sent count.
func (h *ServerStatsHandler) AddBytesOut(n int64) {
	if h.bytesOut != nil {
		h.bytesOut.Add(n)
	}
}

// AddActiveTunnel adds delta to active tunnel count.
func (h *ServerStatsHandler) AddActiveTunnel(delta int64) {
	if h.activeTunnels != nil {
		h.activeTunnels.Add(delta)
	}
}

// AddTotalTunnel adds delta to total tunnel count.
func (h *ServerStatsHandler) AddTotalTunnel(delta int64) {
	if h.totalTunnels != nil {
		h.totalTunnels.Add(delta)
	}
}

// ClientStatsHandler implements StatsHandler for MuxClient.
type ClientStatsHandler struct {
	activeConns *atomic.Int64
	totalConns  *atomic.Int64
	bytesIn     *atomic.Int64
	bytesOut    *atomic.Int64
}

// NewClientStatsHandler creates a new client stats handler.
func NewClientStatsHandler(
	activeConns, totalConns, bytesIn, bytesOut *atomic.Int64,
) *ClientStatsHandler {
	return &ClientStatsHandler{
		activeConns: activeConns,
		totalConns:  totalConns,
		bytesIn:     bytesIn,
		bytesOut:    bytesOut,
	}
}

// AddActiveConnection adds delta to active connection count.
func (h *ClientStatsHandler) AddActiveConnection(delta int64) {
	if h.activeConns != nil {
		h.activeConns.Add(delta)
	}
}

// AddTotalConnection adds delta to total connection count.
func (h *ClientStatsHandler) AddTotalConnection(delta int64) {
	if h.totalConns != nil {
		h.totalConns.Add(delta)
	}
}

// AddBytesIn adds n to bytes received count.
func (h *ClientStatsHandler) AddBytesIn(n int64) {
	if h.bytesIn != nil {
		h.bytesIn.Add(n)
	}
}

// AddBytesOut adds n to bytes sent count.
func (h *ClientStatsHandler) AddBytesOut(n int64) {
	if h.bytesOut != nil {
		h.bytesOut.Add(n)
	}
}

// AddActiveTunnel is a no-op for client (single tunnel).
func (h *ClientStatsHandler) AddActiveTunnel(delta int64) {}

// AddTotalTunnel is a no-op for client (single tunnel).
func (h *ClientStatsHandler) AddTotalTunnel(delta int64) {}

// NoopStatsHandler is a no-op implementation for testing.
type NoopStatsHandler struct{}

// NewNoopStatsHandler creates a new no-op stats handler.
func NewNoopStatsHandler() *NoopStatsHandler {
	return &NoopStatsHandler{}
}

func (h *NoopStatsHandler) AddActiveConnection(delta int64) {}
func (h *NoopStatsHandler) AddTotalConnection(delta int64)  {}
func (h *NoopStatsHandler) AddBytesIn(n int64)              {}
func (h *NoopStatsHandler) AddBytesOut(n int64)             {}
func (h *NoopStatsHandler) AddActiveTunnel(delta int64)     {}
func (h *NoopStatsHandler) AddTotalTunnel(delta int64)      {}

// Compile-time interface verification
var (
	_ StatsHandler = (*ServerStatsHandler)(nil)
	_ StatsHandler = (*ClientStatsHandler)(nil)
	_ StatsHandler = (*NoopStatsHandler)(nil)
)
