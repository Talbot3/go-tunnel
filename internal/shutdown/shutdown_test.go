package shutdown

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandler_Register(t *testing.T) {
	h := NewHandler(DefaultConfig())

	var executed int32
	h.Register("test1", 1, func(ctx context.Context) error {
		atomic.AddInt32(&executed, 1)
		return nil
	})
	h.Register("test2", 2, func(ctx context.Context) error {
		atomic.AddInt32(&executed, 1)
		return nil
	})

	if len(h.callbacks) != 2 {
		t.Errorf("expected 2 callbacks, got %d", len(h.callbacks))
	}
}

func TestHandler_Shutdown(t *testing.T) {
	h := NewHandler(Config{
		Timeout: 5 * time.Second,
	})

	var executedCount int32
	var wg sync.WaitGroup

	h.Register("last", 100, func(ctx context.Context) error {
		atomic.AddInt32(&executedCount, 1)
		wg.Done()
		return nil
	})

	h.Register("first", 1, func(ctx context.Context) error {
		atomic.AddInt32(&executedCount, 1)
		wg.Done()
		return nil
	})

	h.Register("middle", 50, func(ctx context.Context) error {
		atomic.AddInt32(&executedCount, 1)
		wg.Done()
		return nil
	})

	wg.Add(3)

	// Trigger shutdown
	go func() {
		time.Sleep(10 * time.Millisecond)
		h.quit <- nil
	}()

	h.Wait()

	// All callbacks should have been executed
	if atomic.LoadInt32(&executedCount) != 3 {
		t.Errorf("expected 3 executions, got %d", executedCount)
	}
}

func TestHandler_Timeout(t *testing.T) {
	h := NewHandler(Config{
		Timeout: 100 * time.Millisecond,
	})

	completed := atomic.Bool{}
	h.Register("slow", 1, func(ctx context.Context) error {
		// This should be interrupted by timeout
		time.Sleep(1 * time.Second)
		completed.Store(true)
		return nil
	})

	// Trigger shutdown
	go func() {
		time.Sleep(10 * time.Millisecond)
		h.quit <- nil
	}()

	h.Wait()

	// Should have timed out before completion
	if completed.Load() {
		t.Error("expected callback to be interrupted by timeout")
	}
}

func TestHandler_IsShuttingDown(t *testing.T) {
	h := NewHandler(DefaultConfig())

	if h.IsShuttingDown() {
		t.Error("expected not shutting down initially")
	}

	// Trigger shutdown
	go func() {
		time.Sleep(10 * time.Millisecond)
		h.quit <- nil
	}()

	<-h.quit
	h.Shutdown()

	if !h.IsShuttingDown() {
		t.Error("expected shutting down after Shutdown()")
	}
}

func TestHandler_DoubleShutdown(t *testing.T) {
	h := NewHandler(DefaultConfig())

	var callCount int32
	h.Register("test", 1, func(ctx context.Context) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	})

	// Call shutdown twice
	h.Shutdown()
	h.Shutdown()

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected callback to be called once, got %d", callCount)
	}
}

func TestGlobal(t *testing.T) {
	Init(DefaultConfig())

	var executed int32
	Register("global-test", 1, func(ctx context.Context) error {
		atomic.AddInt32(&executed, 1)
		return nil
	})

	if !IsShuttingDown() {
		// Should not be shutting down yet
	}

	// Note: We don't call Wait() or Shutdown() here as it would affect the global state
	_ = executed
}

func TestSortCallbacks(t *testing.T) {
	callbacks := []Callback{
		{Name: "c", Priority: 3},
		{Name: "a", Priority: 1},
		{Name: "b", Priority: 2},
	}

	sortCallbacks(callbacks)

	if callbacks[0].Name != "a" {
		t.Errorf("expected first to be 'a', got '%s'", callbacks[0].Name)
	}
	if callbacks[1].Name != "b" {
		t.Errorf("expected second to be 'b', got '%s'", callbacks[1].Name)
	}
	if callbacks[2].Name != "c" {
		t.Errorf("expected third to be 'c', got '%s'", callbacks[2].Name)
	}
}

func TestHandler_ContextCancellation(t *testing.T) {
	h := NewHandler(Config{
		Timeout: 5 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)

	var err error
	go func() {
		defer wg.Done()
		err = h.WaitWithContext(ctx)
	}()

	// Cancel context
	time.Sleep(50 * time.Millisecond)
	cancel()

	wg.Wait()

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
