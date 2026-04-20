// Package circuit provides circuit breaker pattern implementation for high availability.
package circuit

// BreakerInterface defines the circuit breaker interface for dependency injection.
// This allows different implementations to be injected into components that need them.
type BreakerInterface interface {
	// Allow checks if a request should be allowed through.
	// Returns an error if the circuit is open.
	Allow() error

	// RecordSuccess records a successful operation.
	RecordSuccess()

	// RecordFailure records a failed operation.
	RecordFailure()

	// State returns the current state of the circuit breaker.
	State() State

	// Reset resets the circuit breaker to closed state.
	Reset()
}

// Compile-time interface verification
var _ BreakerInterface = (*Breaker)(nil)
