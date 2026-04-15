// Package pool provides connection pooling for client scenarios.
//
// This package implements connection reuse for high-throughput client
// deployments where connections are long-lived and reused frequently.
package pool

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Errors returned by the connection pool.
var (
	// ErrPoolClosed is returned when the pool is closed.
	ErrPoolClosed = errors.New("connection pool closed")
	// ErrPoolExhausted is returned when the pool has no available connections.
	ErrPoolExhausted = errors.New("connection pool exhausted")
)

// ConnPoolConfig holds the configuration for the connection pool.
type ConnPoolConfig struct {
	// MaxIdle is the maximum number of idle connections.
	// Default is 10.
	MaxIdle int

	// MaxAge is the maximum age of a connection.
	// Connections older than this are closed.
	// Default is 0 (no limit).
	MaxAge time.Duration

	// DialTimeout is the timeout for creating new connections.
	// Default is 10 seconds.
	DialTimeout time.Duration

	// KeepAlive is the keep-alive interval for connections.
	// Default is 30 seconds.
	KeepAlive time.Duration
}

// DefaultConnPoolConfig returns the default configuration.
func DefaultConnPoolConfig() ConnPoolConfig {
	return ConnPoolConfig{
		MaxIdle:     10,
		MaxAge:      0,
		DialTimeout: 10 * time.Second,
		KeepAlive:   30 * time.Second,
	}
}

// ConnPool is a pool of reusable connections.
type ConnPool struct {
	config ConnPoolConfig

	// Factory function to create new connections
	factory func() (net.Conn, error)

	// Idle connections
	mu    sync.Mutex
	conns []*pooledConn

	// Statistics
	created    atomic.Int64
	reused     atomic.Int64
	closed     atomic.Int64
	waitCount  atomic.Int64
	waitTime   atomic.Int64

	// Lifecycle
	closed_ atomic.Bool
}

type pooledConn struct {
	net.Conn
	createdAt time.Time
	pool      *ConnPool
	closed    atomic.Bool
}

// NewConnPool creates a new connection pool.
func NewConnPool(factory func() (net.Conn, error), cfg ConnPoolConfig) *ConnPool {
	if cfg.MaxIdle <= 0 {
		cfg.MaxIdle = 10
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 10 * time.Second
	}
	if cfg.KeepAlive <= 0 {
		cfg.KeepAlive = 30 * time.Second
	}

	return &ConnPool{
		config:  cfg,
		factory: factory,
		conns:   make([]*pooledConn, 0, cfg.MaxIdle),
	}
}

// Get retrieves a connection from the pool or creates a new one.
func (p *ConnPool) Get(ctx context.Context) (net.Conn, error) {
	if p.closed_.Load() {
		return nil, ErrPoolClosed
	}

	// Try to get an idle connection
	p.mu.Lock()
	for len(p.conns) > 0 {
		pc := p.conns[len(p.conns)-1]
		p.conns = p.conns[:len(p.conns)-1]
		p.mu.Unlock()

		// Check if connection is expired
		if p.config.MaxAge > 0 && time.Since(pc.createdAt) > p.config.MaxAge {
			pc.Conn.Close()
			pc.closed.Store(true)
			p.closed.Add(1)
			p.mu.Lock()
			continue
		}

		p.reused.Add(1)
		return pc, nil
	}
	p.mu.Unlock()

	// Create a new connection
	created, err := p.createConn(ctx)
	if err != nil {
		return nil, err
	}

	p.created.Add(1)
	return created, nil
}

// Put returns a connection to the pool.
func (p *ConnPool) Put(conn net.Conn) {
	if conn == nil {
		return
	}

	if p.closed_.Load() {
		conn.Close()
		p.closed.Add(1)
		return
	}

	pc, ok := conn.(*pooledConn)
	if !ok {
		// Not a pooled connection, just close it
		conn.Close()
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.conns) >= p.config.MaxIdle {
		pc.Conn.Close()
		pc.closed.Store(true)
		p.closed.Add(1)
		return
	}

	p.conns = append(p.conns, pc)
}

// Close closes all connections in the pool.
func (p *ConnPool) Close() error {
	if !p.closed_.CompareAndSwap(false, true) {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pc := range p.conns {
		pc.Conn.Close()
		pc.closed.Store(true)
		p.closed.Add(1)
	}
	p.conns = nil

	return nil
}

// Stats returns pool statistics.
type Stats struct {
	Created   int64
	Reused    int64
	Closed    int64
	Idle      int
	MaxIdle   int
	WaitCount int64
	WaitTime  time.Duration
}

// GetStats returns current pool statistics.
func (p *ConnPool) GetStats() Stats {
	p.mu.Lock()
	idle := len(p.conns)
	p.mu.Unlock()

	return Stats{
		Created:   p.created.Load(),
		Reused:    p.reused.Load(),
		Closed:    p.closed.Load(),
		Idle:      idle,
		MaxIdle:   p.config.MaxIdle,
		WaitCount: p.waitCount.Load(),
		WaitTime:  time.Duration(p.waitTime.Load()),
	}
}

// createConn creates a new connection with timeout.
func (p *ConnPool) createConn(ctx context.Context) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	done := make(chan result, 1)

	go func() {
		conn, err := p.factory()
		done <- result{conn, err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			return nil, r.err
		}
		return &pooledConn{
			Conn:      r.conn,
			createdAt: time.Now(),
			pool:      p,
		}, nil
	case <-ctx.Done():
		// The factory goroutine will still complete, but we need to handle
		// the connection if it was created successfully
		go func() {
			r := <-done
			if r.conn != nil {
				r.conn.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

// Prune removes expired connections from the pool.
func (p *ConnPool) Prune() int {
	if p.config.MaxAge <= 0 {
		return 0
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	var pruned int

	// Filter out expired connections
	active := p.conns[:0]
	for _, pc := range p.conns {
		if now.Sub(pc.createdAt) > p.config.MaxAge {
			pc.Conn.Close()
			pc.closed.Store(true)
			p.closed.Add(1)
			pruned++
		} else {
			active = append(active, pc)
		}
	}
	p.conns = active

	return pruned
}

// WrappedConn methods

func (pc *pooledConn) Close() error {
	// Return to pool instead of closing
	if pc.pool != nil && !pc.pool.closed_.Load() {
		// Reset closed flag so it can be reused
		pc.closed.Store(false)
		pc.pool.Put(pc)
		return nil
	}

	if pc.closed.Swap(true) {
		return nil
	}

	return pc.Conn.Close()
}

// ForceClose forcibly closes the connection without returning to pool.
func (pc *pooledConn) ForceClose() error {
	pc.closed.Store(true)
	return pc.Conn.Close()
}

// Age returns how long the connection has existed.
func (pc *pooledConn) Age() time.Duration {
	return time.Since(pc.createdAt)
}

// Dialer creates a connection pool for a specific address.
type Dialer struct {
	network string
	address string
	config  ConnPoolConfig
	dialer  *net.Dialer
}

// NewDialer creates a new dialer with connection pooling.
func NewDialer(network, address string, cfg ConnPoolConfig) *Dialer {
	return &Dialer{
		network: network,
		address: address,
		config:  cfg,
		dialer: &net.Dialer{
			Timeout:   cfg.DialTimeout,
			KeepAlive: cfg.KeepAlive,
		},
	}
}

// NewPool creates a connection pool for this dialer.
func (d *Dialer) NewPool() *ConnPool {
	return NewConnPool(func() (net.Conn, error) {
		return d.dialer.Dial(d.network, d.address)
	}, d.config)
}

// Dial creates a new connection (not pooled).
func (d *Dialer) Dial(ctx context.Context) (net.Conn, error) {
	return d.dialer.DialContext(ctx, d.network, d.address)
}
