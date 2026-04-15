package limiter

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConnectionLimiter(t *testing.T) {
	limiter := NewConnectionLimiter(3)

	// Acquire 3 slots
	for i := 0; i < 3; i++ {
		if err := limiter.Acquire(); err != nil {
			t.Errorf("expected no error for acquire %d, got %v", i, err)
		}
	}

	// 4th should fail
	if err := limiter.Acquire(); err != ErrLimitExceeded {
		t.Errorf("expected ErrLimitExceeded, got %v", err)
	}

	// Current should be 3
	if limiter.Current() != 3 {
		t.Errorf("expected current to be 3, got %d", limiter.Current())
	}

	// Release one
	limiter.Release()

	// Should be able to acquire now
	if err := limiter.Acquire(); err != nil {
		t.Errorf("expected no error after release, got %v", err)
	}
}

func TestConnectionLimiter_Concurrent(t *testing.T) {
	limiter := NewConnectionLimiter(100)

	var wg sync.WaitGroup
	const goroutines = 1000
	const iterations = 100

	var acquired int64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if err := limiter.Acquire(); err == nil {
					atomic.AddInt64(&acquired, 1)
					limiter.Release()
				}
			}
		}()
	}

	wg.Wait()

	// Should have acquired some slots
	if acquired == 0 {
		t.Error("expected some successful acquires")
	}
}

func TestRateLimiter(t *testing.T) {
	// 10 operations per 100ms
	limiter := NewRateLimiter(10, 100*time.Millisecond)

	// Should allow first 10
	for i := 0; i < 10; i++ {
		if !limiter.Allow() {
			t.Errorf("expected allow for request %d", i)
		}
	}

	// 11th should be denied
	if limiter.Allow() {
		t.Error("expected rate limit to be exceeded")
	}

	// Wait for refill
	time.Sleep(150 * time.Millisecond)

	// Should allow again
	if !limiter.Allow() {
		t.Error("expected allow after refill")
	}
}

func TestRateLimiter_Wait(t *testing.T) {
	limiter := NewRateLimiter(1, 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Use up the token
	limiter.Allow()

	// Wait should succeed after refill
	err := limiter.Wait(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestRateLimiter_WaitContextCancellation(t *testing.T) {
	limiter := NewRateLimiter(1, 1*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Use up the token
	limiter.Allow()

	// Wait should fail due to context cancellation
	err := limiter.Wait(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestGoroutineLimiter(t *testing.T) {
	limiter := NewGoroutineLimiter(3)

	var running int64
	var wg sync.WaitGroup

	// Start 3 goroutines
	for i := 0; i < 3; i++ {
		wg.Add(1)
		if err := limiter.Go(func() {
			atomic.AddInt64(&running, 1)
			time.Sleep(100 * time.Millisecond)
			atomic.AddInt64(&running, -1)
			wg.Done()
		}); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	}

	// 4th should fail
	if err := limiter.Go(func() {}); err != ErrLimitExceeded {
		t.Errorf("expected ErrLimitExceeded, got %v", err)
	}

	// Wait for completion
	wg.Wait()

	// Current should be 0
	if limiter.Current() != 0 {
		t.Errorf("expected current to be 0, got %d", limiter.Current())
	}
}

func TestMemoryLimiter(t *testing.T) {
	limiter := NewMemoryLimiter(1000)

	// Allocate 500 bytes
	if err := limiter.Allocate(500); err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Check used
	if limiter.Used() != 500 {
		t.Errorf("expected 500 used, got %d", limiter.Used())
	}

	// Allocate another 600 should fail
	if err := limiter.Allocate(600); err != ErrLimitExceeded {
		t.Errorf("expected ErrLimitExceeded, got %v", err)
	}

	// Allocate 400 should succeed
	if err := limiter.Allocate(400); err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Release 300
	limiter.Release(300)

	// Check used
	if limiter.Used() != 600 {
		t.Errorf("expected 600 used, got %d", limiter.Used())
	}

	// Check available
	if limiter.Available() != 400 {
		t.Errorf("expected 400 available, got %d", limiter.Available())
	}
}

func TestCompositeLimiter(t *testing.T) {
	connLimiter := NewConnectionLimiter(5)
	goLimiter := NewGoroutineLimiter(5)

	composite := NewCompositeLimiter(connLimiter, goLimiter)

	// Acquire
	if err := composite.Acquire(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Both should be incremented
	if connLimiter.Current() != 1 {
		t.Errorf("expected 1 connection, got %d", connLimiter.Current())
	}
	if goLimiter.Current() != 1 {
		t.Errorf("expected 1 goroutine, got %d", goLimiter.Current())
	}

	// Release
	composite.Release()

	// Both should be decremented
	if connLimiter.Current() != 0 {
		t.Errorf("expected 0 connections, got %d", connLimiter.Current())
	}
	if goLimiter.Current() != 0 {
		t.Errorf("expected 0 goroutines, got %d", goLimiter.Current())
	}
}

func TestResourceMonitor(t *testing.T) {
	monitor := NewResourceMonitor(100, 50, 1024*1024, 1000, time.Second)

	stats := monitor.Stats()

	if stats.MaxConnections != 100 {
		t.Errorf("expected max connections 100, got %d", stats.MaxConnections)
	}
	if stats.MaxGoroutines != 50 {
		t.Errorf("expected max goroutines 50, got %d", stats.MaxGoroutines)
	}
	if stats.MaxMemory != 1024*1024 {
		t.Errorf("expected max memory 1MB, got %d", stats.MaxMemory)
	}
}

// BenchmarkConnectionLimiter_Acquire benchmarks the Acquire method.
func BenchmarkConnectionLimiter_Acquire(b *testing.B) {
	limiter := NewConnectionLimiter(int64(b.N + 1))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Acquire()
		limiter.Release()
	}
}

// BenchmarkRateLimiter_Allow benchmarks the Allow method.
func BenchmarkRateLimiter_Allow(b *testing.B) {
	limiter := NewRateLimiter(int64(b.N+1), time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow()
	}
}
