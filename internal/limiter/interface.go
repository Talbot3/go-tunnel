// Package limiter provides resource limiting mechanisms for high availability.
package limiter

// Limiter defines the interface for resource limiting.
// This allows different implementations (connection limiter, rate limiter, etc.)
// to be injected into components that need them.
type Limiter interface {
	// Acquire attempts to acquire a resource slot.
	// Returns ErrLimitExceeded if the limit is reached.
	Acquire() error

	// Release releases a resource slot.
	Release()
}

// Compile-time interface verification
var (
	_ Limiter = (*ConnectionLimiter)(nil)
	_ Limiter = (*GoroutineLimiter)(nil)
	_ Limiter = (*CompositeLimiter)(nil)
)
