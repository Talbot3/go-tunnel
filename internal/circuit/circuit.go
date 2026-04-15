// Package circuit provides circuit breaker pattern implementation for high availability.
package circuit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the circuit breaker state.
type State int32

const (
	// StateClosed means requests are allowed through.
	StateClosed State = iota
	// StateOpen means requests are blocked.
	StateOpen
	// StateHalfOpen means limited requests are allowed to test recovery.
	StateHalfOpen
)

// String returns the string representation of the state.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Errors
var (
	ErrCircuitOpen    = errors.New("circuit breaker is open")
	ErrTooManyRequests = errors.New("too many requests in half-open state")
)

// Config holds circuit breaker configuration.
type Config struct {
	// FailureThreshold is the number of failures before opening the circuit.
	// Default: 5
	FailureThreshold int

	// SuccessThreshold is the number of successes before closing the circuit.
	// Default: 3
	SuccessThreshold int

	// Timeout is how long to wait before attempting recovery.
	// Default: 30 seconds
	Timeout time.Duration

	// MaxRequests is the maximum requests allowed in half-open state.
	// Default: 1
	MaxRequests int

	// OnStateChange is called when the state changes.
	OnStateChange func(from, to State)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		Timeout:          30 * time.Second,
		MaxRequests:      1,
	}
}

// Breaker implements the circuit breaker pattern.
type Breaker struct {
	config Config

	// State (atomic for lock-free reads)
	state atomic.Int32

	// Failure and success counters
	failures  atomic.Int64
	successes atomic.Int64

	// Last failure time for timeout calculation
	lastFailureTime atomic.Int64 // UnixNano

	// Request counter for half-open state
	requests atomic.Int64

	// Mutex for state transitions
	mu sync.Mutex
}

// NewBreaker creates a new circuit breaker with the given configuration.
func NewBreaker(config Config) *Breaker {
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = 5
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = 3
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxRequests <= 0 {
		config.MaxRequests = 1
	}

	b := &Breaker{
		config: config,
	}
	b.state.Store(int32(StateClosed))
	return b
}

// State returns the current state of the circuit breaker.
func (b *Breaker) State() State {
	return State(b.state.Load())
}

// Allow checks if a request should be allowed through.
// Returns an error if the circuit is open.
func (b *Breaker) Allow() error {
	state := b.State()

	switch state {
	case StateClosed:
		return nil

	case StateOpen:
		// Check if timeout has elapsed
		lastFailure := time.Unix(0, b.lastFailureTime.Load())
		if time.Since(lastFailure) > b.config.Timeout {
			b.mu.Lock()
			defer b.mu.Unlock()

			// Double-check after acquiring lock
			if b.State() == StateOpen {
				b.transition(StateHalfOpen)
			}
			return nil
		}
		return ErrCircuitOpen

	case StateHalfOpen:
		// Allow limited requests
		if b.requests.Add(1) > int64(b.config.MaxRequests) {
			return ErrTooManyRequests
		}
		return nil

	default:
		return ErrCircuitOpen
	}
}

// Execute runs the given function with circuit breaker protection.
func (b *Breaker) Execute(ctx context.Context, fn func() error) error {
	if err := b.Allow(); err != nil {
		return err
	}

	err := fn()

	if err != nil {
		b.RecordFailure()
		return err
	}

	b.RecordSuccess()
	return nil
}

// RecordSuccess records a successful operation.
func (b *Breaker) RecordSuccess() {
	state := b.State()

	switch state {
	case StateClosed:
		// Reset failure counter on success
		b.failures.Store(0)

	case StateHalfOpen:
		if b.successes.Add(1) >= int64(b.config.SuccessThreshold) {
			b.mu.Lock()
			defer b.mu.Unlock()

			if b.State() == StateHalfOpen {
				b.transition(StateClosed)
			}
		}
	}
}

// RecordFailure records a failed operation.
func (b *Breaker) RecordFailure() {
	state := b.State()

	b.lastFailureTime.Store(time.Now().UnixNano())

	switch state {
	case StateClosed:
		if b.failures.Add(1) >= int64(b.config.FailureThreshold) {
			b.mu.Lock()
			defer b.mu.Unlock()

			if b.State() == StateClosed {
				b.transition(StateOpen)
			}
		}

	case StateHalfOpen:
		b.mu.Lock()
		defer b.mu.Unlock()

		if b.State() == StateHalfOpen {
			b.transition(StateOpen)
		}
	}
}

// transition changes the state and calls the callback.
// Must be called with mu held.
func (b *Breaker) transition(to State) {
	from := b.State()
	if from == to {
		return
	}

	b.state.Store(int32(to))

	// Reset counters
	b.failures.Store(0)
	b.successes.Store(0)
	b.requests.Store(0)

	// Call callback if set
	if b.config.OnStateChange != nil {
		b.config.OnStateChange(from, to)
	}
}

// Stats returns current circuit breaker statistics.
type Stats struct {
	State     State
	Failures  int64
	Successes int64
	Requests  int64
}

// GetStats returns current statistics.
func (b *Breaker) GetStats() Stats {
	return Stats{
		State:     b.State(),
		Failures:  b.failures.Load(),
		Successes: b.successes.Load(),
		Requests:  b.requests.Load(),
	}
}

// Reset resets the circuit breaker to closed state.
func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.transition(StateClosed)
}

// ForceOpen forces the circuit breaker to open state.
func (b *Breaker) ForceOpen() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.transition(StateOpen)
}

// Manager manages multiple circuit breakers.
type Manager struct {
	breakers sync.Map // name -> *Breaker
	config   Config
}

// NewManager creates a new circuit breaker manager.
func NewManager(config Config) *Manager {
	return &Manager{
		config: config,
	}
}

// Get returns the circuit breaker for the given name, creating it if necessary.
func (m *Manager) Get(name string) *Breaker {
	if v, ok := m.breakers.Load(name); ok {
		return v.(*Breaker)
	}

	b := NewBreaker(m.config)
	actual, _ := m.breakers.LoadOrStore(name, b)
	return actual.(*Breaker)
}

// Remove removes the circuit breaker for the given name.
func (m *Manager) Remove(name string) {
	m.breakers.Delete(name)
}

// Range iterates over all circuit breakers.
func (m *Manager) Range(fn func(name string, b *Breaker) bool) {
	m.breakers.Range(func(key, value interface{}) bool {
		return fn(key.(string), value.(*Breaker))
	})
}
