package circuit

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBreaker_ClosedToOpen(t *testing.T) {
	config := Config{
		FailureThreshold: 3,
		Timeout:          1 * time.Second,
	}

	b := NewBreaker(config)

	if b.State() != StateClosed {
		t.Errorf("expected initial state to be closed, got %v", b.State())
	}

	// Record failures
	for i := 0; i < 3; i++ {
		b.RecordFailure()
	}

	if b.State() != StateOpen {
		t.Errorf("expected state to be open after %d failures, got %v", config.FailureThreshold, b.State())
	}

	// Should reject requests
	if err := b.Allow(); err != ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestBreaker_OpenToHalfOpen(t *testing.T) {
	config := Config{
		FailureThreshold: 1,
		Timeout:          100 * time.Millisecond,
	}

	b := NewBreaker(config)

	// Open the circuit
	b.RecordFailure()

	if b.State() != StateOpen {
		t.Errorf("expected state to be open, got %v", b.State())
	}

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Should transition to half-open
	if err := b.Allow(); err != nil {
		t.Errorf("expected no error after timeout, got %v", err)
	}

	if b.State() != StateHalfOpen {
		t.Errorf("expected state to be half-open, got %v", b.State())
	}
}

func TestBreaker_HalfOpenToClosed(t *testing.T) {
	config := Config{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	}

	b := NewBreaker(config)

	// Open the circuit
	b.RecordFailure()

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Transition to half-open
	b.Allow()

	if b.State() != StateHalfOpen {
		t.Errorf("expected state to be half-open, got %v", b.State())
	}

	// Record successes
	b.RecordSuccess()
	b.RecordSuccess()

	if b.State() != StateClosed {
		t.Errorf("expected state to be closed after %d successes, got %v", config.SuccessThreshold, b.State())
	}
}

func TestBreaker_HalfOpenToOpen(t *testing.T) {
	config := Config{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	}

	b := NewBreaker(config)

	// Open the circuit
	b.RecordFailure()

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Transition to half-open
	b.Allow()

	if b.State() != StateHalfOpen {
		t.Errorf("expected state to be half-open, got %v", b.State())
	}

	// Record failure - should go back to open
	b.RecordFailure()

	if b.State() != StateOpen {
		t.Errorf("expected state to be open after failure in half-open, got %v", b.State())
	}
}

func TestBreaker_Execute(t *testing.T) {
	config := Config{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          100 * time.Millisecond,
	}

	b := NewBreaker(config)

	// Successful execution
	err := b.Execute(nil, func() error {
		return nil
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Failed executions
	failingFn := func() error {
		return errors.New("operation failed")
	}

	b.Execute(nil, failingFn)
	b.Execute(nil, failingFn)

	// Should be open now
	if b.State() != StateOpen {
		t.Errorf("expected state to be open, got %v", b.State())
	}

	// Should reject
	err = b.Execute(nil, func() error {
		return nil
	})
	if err != ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestBreaker_Reset(t *testing.T) {
	config := Config{
		FailureThreshold: 1,
		Timeout:          1 * time.Second,
	}

	b := NewBreaker(config)

	// Open the circuit
	b.RecordFailure()
	b.RecordFailure()

	if b.State() != StateOpen {
		t.Errorf("expected state to be open, got %v", b.State())
	}

	// Reset
	b.Reset()

	if b.State() != StateClosed {
		t.Errorf("expected state to be closed after reset, got %v", b.State())
	}
}

func TestBreaker_StateChangeCallback(t *testing.T) {
	var stateChanges []State
	config := Config{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		Timeout:          50 * time.Millisecond,
		OnStateChange: func(from, to State) {
			stateChanges = append(stateChanges, from, to)
		},
	}

	b := NewBreaker(config)

	// Open the circuit
	b.RecordFailure()

	// Wait and transition to half-open
	time.Sleep(100 * time.Millisecond)
	b.Allow()

	// Close the circuit
	b.RecordSuccess()

	// Check state changes: Closed->Open, Open->HalfOpen, HalfOpen->Closed
	expectedChanges := []State{
		StateClosed, StateOpen,
		StateOpen, StateHalfOpen,
		StateHalfOpen, StateClosed,
	}

	if len(stateChanges) != len(expectedChanges) {
		t.Fatalf("expected %d state changes, got %d", len(expectedChanges), len(stateChanges))
	}

	for i, expected := range expectedChanges {
		if stateChanges[i] != expected {
			t.Errorf("state change %d: expected %v, got %v", i, expected, stateChanges[i])
		}
	}
}

func TestManager(t *testing.T) {
	config := DefaultConfig()
	m := NewManager(config)

	b1 := m.Get("service1")
	b2 := m.Get("service2")
	b1Again := m.Get("service1")

	if b1 != b1Again {
		t.Error("expected same breaker for same name")
	}

	if b1 == b2 {
		t.Error("expected different breakers for different names")
	}

	// Remove and get new
	m.Remove("service1")
	b1New := m.Get("service1")

	if b1 == b1New {
		t.Error("expected new breaker after remove")
	}
}

func TestBreaker_ConcurrentAccess(t *testing.T) {
	config := Config{
		FailureThreshold: 100,
		SuccessThreshold: 100,
		Timeout:          1 * time.Second,
	}

	b := NewBreaker(config)

	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 1000

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				b.Allow()
				b.RecordSuccess()
				b.RecordFailure()
				_ = b.State()
				_ = b.GetStats()
			}
		}()
	}

	wg.Wait()
	// If we get here without race conditions, the test passes
}

// BenchmarkBreaker_Allow benchmarks the Allow method.
func BenchmarkBreaker_Allow(b *testing.B) {
	breaker := NewBreaker(DefaultConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		breaker.Allow()
	}
}

// BenchmarkBreaker_RecordSuccess benchmarks RecordSuccess.
func BenchmarkBreaker_RecordSuccess(b *testing.B) {
	breaker := NewBreaker(DefaultConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		breaker.RecordSuccess()
	}
}

// BenchmarkBreaker_Execute benchmarks Execute.
func BenchmarkBreaker_Execute(b *testing.B) {
	breaker := NewBreaker(DefaultConfig())
	fn := func() error { return nil }

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		breaker.Execute(nil, fn)
	}
}
