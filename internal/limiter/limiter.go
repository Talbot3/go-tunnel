// Package limiter provides resource limiting mechanisms for high availability.
package limiter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Errors
var (
	ErrLimitExceeded    = errors.New("resource limit exceeded")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

// ConnectionLimiter limits the number of concurrent connections.
type ConnectionLimiter struct {
	max     int64
	current atomic.Int64
}

// NewConnectionLimiter creates a new connection limiter.
func NewConnectionLimiter(max int64) *ConnectionLimiter {
	return &ConnectionLimiter{
		max: max,
	}
}

// Acquire attempts to acquire a connection slot.
// Returns ErrLimitExceeded if the limit is reached.
func (l *ConnectionLimiter) Acquire() error {
	for {
		current := l.current.Load()
		if current >= l.max {
			return ErrLimitExceeded
		}
		if l.current.CompareAndSwap(current, current+1) {
			return nil
		}
	}
}

// Release releases a connection slot.
func (l *ConnectionLimiter) Release() {
	for {
		current := l.current.Load()
		if current <= 0 {
			return
		}
		if l.current.CompareAndSwap(current, current-1) {
			return
		}
	}
}

// Current returns the current number of connections.
func (l *ConnectionLimiter) Current() int64 {
	return l.current.Load()
}

// Available returns the number of available slots.
func (l *ConnectionLimiter) Available() int64 {
	return l.max - l.current.Load()
}

// RateLimiter implements a token bucket rate limiter.
type RateLimiter struct {
	rate      int64         // Tokens added per interval
	interval  time.Duration // Interval for adding tokens
	maxTokens int64         // Maximum tokens
	tokens    atomic.Int64
	lastTime  atomic.Int64 // UnixNano

	mu sync.Mutex
}

// NewRateLimiter creates a new rate limiter.
// rate is the number of operations allowed per interval.
func NewRateLimiter(rate int64, interval time.Duration) *RateLimiter {
	rl := &RateLimiter{
		rate:      rate,
		interval:  interval,
		maxTokens: rate,
	}
	rl.tokens.Store(rate)
	rl.lastTime.Store(time.Now().UnixNano())
	return rl
}

// Allow checks if an operation is allowed.
func (rl *RateLimiter) Allow() bool {
	return rl.AllowN(1)
}

// AllowN checks if n operations are allowed.
func (rl *RateLimiter) AllowN(n int64) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Refill tokens
	now := time.Now().UnixNano()
	lastTime := rl.lastTime.Load()
	elapsed := time.Duration(now - lastTime)

	tokensToAdd := int64(elapsed/rl.interval) * rl.rate
	if tokensToAdd > 0 {
		currentTokens := rl.tokens.Load()
		newTokens := currentTokens + tokensToAdd
		if newTokens > rl.maxTokens {
			newTokens = rl.maxTokens
		}
		rl.tokens.Store(newTokens)
		rl.lastTime.Store(now)
	}

	// Check if we have enough tokens
	currentTokens := rl.tokens.Load()
	if currentTokens < n {
		return false
	}

	rl.tokens.Store(currentTokens - n)
	return true
}

// Wait blocks until an operation is allowed or context is canceled.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	return rl.WaitN(ctx, 1)
}

// WaitN blocks until n operations are allowed or context is canceled.
func (rl *RateLimiter) WaitN(ctx context.Context, n int64) error {
	for {
		if rl.AllowN(n) {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(rl.interval / time.Duration(rl.rate)):
		}
	}
}

// Tokens returns the current number of tokens.
func (rl *RateLimiter) Tokens() int64 {
	return rl.tokens.Load()
}

// GoroutineLimiter limits the number of concurrent goroutines.
type GoroutineLimiter struct {
	max     int64
	current atomic.Int64
}

// NewGoroutineLimiter creates a new goroutine limiter.
func NewGoroutineLimiter(max int64) *GoroutineLimiter {
	return &GoroutineLimiter{
		max: max,
	}
}

// Go runs a function in a new goroutine if the limit is not exceeded.
func (l *GoroutineLimiter) Go(fn func()) error {
	if err := l.Acquire(); err != nil {
		return err
	}

	go func() {
		defer l.Release()
		fn()
	}()

	return nil
}

// Acquire acquires a goroutine slot.
func (l *GoroutineLimiter) Acquire() error {
	current := l.current.Load()
	if current >= l.max {
		return ErrLimitExceeded
	}
	if l.current.CompareAndSwap(current, current+1) {
		return nil
	}
	// Retry
	return l.Acquire()
}

// Release releases a goroutine slot.
func (l *GoroutineLimiter) Release() {
	for {
		current := l.current.Load()
		if current <= 0 {
			return
		}
		if l.current.CompareAndSwap(current, current-1) {
			return
		}
	}
}

// Current returns the current number of goroutines.
func (l *GoroutineLimiter) Current() int64 {
	return l.current.Load()
}

// MemoryLimiter tracks memory usage.
type MemoryLimiter struct {
	maxBytes   int64
	usedBytes  atomic.Int64
	allocCount atomic.Int64
}

// NewMemoryLimiter creates a new memory limiter.
func NewMemoryLimiter(maxBytes int64) *MemoryLimiter {
	return &MemoryLimiter{
		maxBytes: maxBytes,
	}
}

// Allocate attempts to allocate the given number of bytes.
func (l *MemoryLimiter) Allocate(bytes int64) error {
	for {
		current := l.usedBytes.Load()
		if current+bytes > l.maxBytes {
			return ErrLimitExceeded
		}
		if l.usedBytes.CompareAndSwap(current, current+bytes) {
			l.allocCount.Add(1)
			return nil
		}
	}
}

// Release releases the given number of bytes.
func (l *MemoryLimiter) Release(bytes int64) {
	for {
		current := l.usedBytes.Load()
		newVal := current - bytes
		if newVal < 0 {
			newVal = 0
		}
		if l.usedBytes.CompareAndSwap(current, newVal) {
			return
		}
	}
}

// Used returns the currently used bytes.
func (l *MemoryLimiter) Used() int64 {
	return l.usedBytes.Load()
}

// Available returns the available bytes.
func (l *MemoryLimiter) Available() int64 {
	return l.maxBytes - l.usedBytes.Load()
}

// UsagePercent returns the usage as a percentage.
func (l *MemoryLimiter) UsagePercent() float64 {
	return float64(l.usedBytes.Load()) / float64(l.maxBytes) * 100
}

// CompositeLimiter combines multiple limiters.
type CompositeLimiter struct {
	limiters []interface {
		Acquire() error
		Release()
	}
}

// NewCompositeLimiter creates a new composite limiter.
func NewCompositeLimiter(limiters ...interface {
	Acquire() error
	Release()
}) *CompositeLimiter {
	return &CompositeLimiter{
		limiters: limiters,
	}
}

// Acquire acquires all limiters.
func (l *CompositeLimiter) Acquire() error {
	// Try to acquire all limiters
	acquired := make([]int, 0)
	for i, limiter := range l.limiters {
		if err := limiter.Acquire(); err != nil {
			// Release already acquired limiters
			for _, j := range acquired {
				l.limiters[j].Release()
			}
			return err
		}
		acquired = append(acquired, i)
	}
	return nil
}

// Release releases all limiters.
func (l *CompositeLimiter) Release() {
	for _, limiter := range l.limiters {
		limiter.Release()
	}
}

// Stats returns statistics for all limiters.
type Stats struct {
	Connections     int64
	MaxConnections  int64
	Goroutines      int64
	MaxGoroutines   int64
	MemoryUsed      int64
	MaxMemory       int64
	RateLimitTokens int64
}

// ResourceMonitor monitors resource usage.
type ResourceMonitor struct {
	connLimiter *ConnectionLimiter
	goLimiter   *GoroutineLimiter
	memLimiter  *MemoryLimiter
	rateLimiter *RateLimiter
}

// NewResourceMonitor creates a new resource monitor.
func NewResourceMonitor(maxConns, maxGoroutines, maxMemory int64, rateLimit int64, rateInterval time.Duration) *ResourceMonitor {
	return &ResourceMonitor{
		connLimiter: NewConnectionLimiter(maxConns),
		goLimiter:   NewGoroutineLimiter(maxGoroutines),
		memLimiter:  NewMemoryLimiter(maxMemory),
		rateLimiter: NewRateLimiter(rateLimit, rateInterval),
	}
}

// Stats returns current resource statistics.
func (m *ResourceMonitor) Stats() Stats {
	return Stats{
		Connections:     m.connLimiter.Current(),
		MaxConnections:  m.connLimiter.max,
		Goroutines:      m.goLimiter.Current(),
		MaxGoroutines:   m.goLimiter.max,
		MemoryUsed:      m.memLimiter.Used(),
		MaxMemory:       m.memLimiter.maxBytes,
		RateLimitTokens: m.rateLimiter.Tokens(),
	}
}
