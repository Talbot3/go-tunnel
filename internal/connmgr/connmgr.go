// Package connmgr provides connection management for server scenarios.
//
// This package implements connection limiting, tracking, and lifecycle management
// for high-concurrency server deployments.
package connmgr

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Errors returned by the connection manager.
var (
	// ErrConnectionLimit is returned when the connection limit is reached.
	ErrConnectionLimit = errors.New("connection limit reached")
	// ErrManagerClosed is returned when the manager is closed.
	ErrManagerClosed = errors.New("connection manager closed")
)

// Config holds the configuration for the connection manager.
type Config struct {
	// MaxConnections is the maximum number of concurrent connections.
	// 0 means unlimited.
	MaxConnections int

	// ConnectionTimeout is the idle timeout for connections.
	// 0 means no timeout.
	ConnectionTimeout time.Duration

	// AcceptTimeout is the timeout for accepting new connections.
	AcceptTimeout time.Duration
}

// ConnInfo holds information about a connection.
type ConnInfo struct {
	ID         string
	RemoteAddr string
	StartTime  time.Time

	// Activity tracking
	lastActive atomic.Int64

	// Traffic tracking
	bytesIn  atomic.Int64
	bytesOut atomic.Int64

	// State
	closed atomic.Bool
}

// LastActive returns the last active time.
func (c *ConnInfo) LastActive() time.Time {
	return time.Unix(0, c.lastActive.Load())
}

// UpdateActive updates the last active time to now.
func (c *ConnInfo) UpdateActive() {
	c.lastActive.Store(time.Now().UnixNano())
}

// BytesIn returns the total bytes received.
func (c *ConnInfo) BytesIn() int64 {
	return c.bytesIn.Load()
}

// AddBytesIn adds to the bytes received counter.
func (c *ConnInfo) AddBytesIn(n int64) {
	c.bytesIn.Add(n)
}

// BytesOut returns the total bytes sent.
func (c *ConnInfo) BytesOut() int64 {
	return c.bytesOut.Load()
}

// AddBytesOut adds to the bytes sent counter.
func (c *ConnInfo) AddBytesOut(n int64) {
	c.bytesOut.Add(n)
}

// Duration returns how long the connection has been active.
func (c *ConnInfo) Duration() time.Duration {
	return time.Since(c.StartTime)
}

// IsClosed returns whether the connection is closed.
func (c *ConnInfo) IsClosed() bool {
	return c.closed.Load()
}

// Close marks the connection as closed.
func (c *ConnInfo) Close() {
	c.closed.Store(true)
}

// Manager manages connections for server scenarios.
type Manager struct {
	config Config

	// Counters
	active   atomic.Int64
	total    atomic.Int64
	rejected atomic.Int64

	// Connection tracking
	connections sync.Map // connID -> *ConnInfo

	// Concurrency control
	semaphore chan struct{}

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewManager creates a new connection manager.
func NewManager(cfg Config) *Manager {
	m := &Manager{
		config: cfg,
	}

	if cfg.MaxConnections > 0 {
		m.semaphore = make(chan struct{}, cfg.MaxConnections)
	}

	m.ctx, m.cancel = context.WithCancel(context.Background())
	return m
}

// Accept processes a new connection.
// Returns ErrConnectionLimit if the connection limit is reached.
func (m *Manager) Accept(conn net.Conn) (*ConnInfo, error) {
	select {
	case <-m.ctx.Done():
		return nil, ErrManagerClosed
	default:
	}

	// Check connection limit
	if m.semaphore != nil {
		select {
		case m.semaphore <- struct{}{}:
			// Got permit
		default:
			m.rejected.Add(1)
			return nil, ErrConnectionLimit
		}
	}

	m.active.Add(1)
	m.total.Add(1)

	info := &ConnInfo{
		ID:         generateID(),
		RemoteAddr: conn.RemoteAddr().String(),
		StartTime:  time.Now(),
	}
	info.UpdateActive()

	m.connections.Store(info.ID, info)

	return info, nil
}

// Release releases a connection.
func (m *Manager) Release(info *ConnInfo) {
	if info == nil {
		return
	}

	info.Close()
	m.active.Add(-1)
	m.connections.Delete(info.ID)

	if m.semaphore != nil {
		<-m.semaphore
	}
}

// Active returns the number of active connections.
func (m *Manager) Active() int64 {
	return m.active.Load()
}

// Total returns the total number of connections accepted.
func (m *Manager) Total() int64 {
	return m.total.Load()
}

// Rejected returns the number of rejected connections.
func (m *Manager) Rejected() int64 {
	return m.rejected.Load()
}

// GetInfo returns the connection info for the given ID.
func (m *Manager) GetInfo(id string) (*ConnInfo, bool) {
	v, ok := m.connections.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*ConnInfo), true
}

// ForEach iterates over all active connections.
func (m *Manager) ForEach(fn func(id string, info *ConnInfo) bool) {
	m.connections.Range(func(key, value interface{}) bool {
		return fn(key.(string), value.(*ConnInfo))
	})
}

// CloseIdle closes connections that have been idle for longer than the timeout.
func (m *Manager) CloseIdle(timeout time.Duration) []string {
	if timeout <= 0 {
		return nil
	}

	var closed []string
	now := time.Now()

	m.connections.Range(func(key, value interface{}) bool {
		info := value.(*ConnInfo)
		if now.Sub(info.LastActive()) > timeout {
			closed = append(closed, info.ID)
		}
		return true
	})

	return closed
}

// Stats returns statistics about the connection manager.
type Stats struct {
	Active    int64
	Total     int64
	Rejected  int64
	Limit     int
	Utilization float64
}

// GetStats returns current statistics.
func (m *Manager) GetStats() Stats {
	active := m.active.Load()
	limit := m.config.MaxConnections
	var utilization float64
	if limit > 0 {
		utilization = float64(active) / float64(limit) * 100
	}

	return Stats{
		Active:    active,
		Total:     m.total.Load(),
		Rejected:  m.rejected.Load(),
		Limit:     limit,
		Utilization: utilization,
	}
}

// Shutdown gracefully shuts down the manager.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.cancel()

	// Wait for all connections to be released or context to be done
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// generateID generates a random connection ID.
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// WrappedConn wraps a net.Conn with connection management.
type WrappedConn struct {
	net.Conn
	info   *ConnInfo
	mgr    *Manager
	closed atomic.Bool
}

// WrapConn wraps a connection with management tracking.
func (m *Manager) WrapConn(conn net.Conn, info *ConnInfo) *WrappedConn {
	return &WrappedConn{
		Conn: conn,
		info: info,
		mgr:  m,
	}
}

// Read reads from the connection and updates tracking.
func (w *WrappedConn) Read(b []byte) (n int, err error) {
	n, err = w.Conn.Read(b)
	if n > 0 {
		w.info.AddBytesIn(int64(n))
		w.info.UpdateActive()
	}
	return
}

// Write writes to the connection and updates tracking.
func (w *WrappedConn) Write(b []byte) (n int, err error) {
	n, err = w.Conn.Write(b)
	if n > 0 {
		w.info.AddBytesOut(int64(n))
		w.info.UpdateActive()
	}
	return
}

// Close closes the connection and releases resources.
func (w *WrappedConn) Close() error {
	if w.closed.Swap(true) {
		return nil
	}
	err := w.Conn.Close()
	w.mgr.Release(w.info)
	return err
}

// Info returns the connection info.
func (w *WrappedConn) Info() *ConnInfo {
	return w.info
}
