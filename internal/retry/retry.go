// Package retry provides exponential backoff retry mechanisms for high availability.
package retry

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"
)

// Config holds retry configuration.
type Config struct {
	// MaxAttempts is the maximum number of retry attempts.
	// 0 means unlimited retries (use with caution).
	// Default: 3
	MaxAttempts int

	// InitialDelay is the initial delay before first retry.
	// Default: 100ms
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries.
	// Default: 30s
	MaxDelay time.Duration

	// Multiplier is the factor by which delay is multiplied after each attempt.
	// Default: 2.0
	Multiplier float64

	// Jitter adds randomness to prevent thundering herd.
	// 0 means no jitter, 1 means full jitter.
	// Default: 0.1
	Jitter float64

	// RetryableError determines if an error is retryable.
	// If nil, all errors are retryable.
	RetryableError func(error) bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.1,
	}
}

// Retryable errors
var (
	ErrMaxAttemptsExceeded = errors.New("maximum retry attempts exceeded")
	ErrContextCanceled     = errors.New("context canceled")
)

// Retryable wraps common errors that should be retried.
var Retryable = func(err error) bool {
	// Common network errors that should be retried
	return isTemporary(err) || isTimeout(err)
}

// isTemporary checks if the error is temporary.
func isTemporary(err error) bool {
	type temporary interface {
		Temporary() bool
	}
	if te, ok := err.(temporary); ok {
		return te.Temporary()
	}
	return false
}

// isTimeout checks if the error is a timeout.
func isTimeout(err error) bool {
	type timeout interface {
		Timeout() bool
	}
	if te, ok := err.(timeout); ok {
		return te.Timeout()
	}
	return false
}

// Retrier implements retry with exponential backoff.
type Retrier struct {
	config Config
	rand   *rand.Rand
	mu     sync.Mutex
}

// NewRetrier creates a new retrier with the given configuration.
func NewRetrier(config Config) *Retrier {
	if config.MaxAttempts < 0 {
		config.MaxAttempts = 0
	}
	if config.InitialDelay <= 0 {
		config.InitialDelay = 100 * time.Millisecond
	}
	if config.MaxDelay <= 0 {
		config.MaxDelay = 30 * time.Second
	}
	if config.Multiplier <= 1 {
		config.Multiplier = 2.0
	}
	if config.Jitter < 0 {
		config.Jitter = 0
	}
	if config.Jitter > 1 {
		config.Jitter = 1
	}

	return &Retrier{
		config: config,
		rand:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Do executes the given function with retry logic.
func (r *Retrier) Do(ctx context.Context, fn func() error) error {
	_, err := r.DoWithResult(ctx, func() (interface{}, error) {
		return nil, fn()
	})
	return err
}

// DoWithResult executes the given function with retry logic and returns a result.
func (r *Retrier) DoWithResult(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	var lastErr error
	var delay time.Duration

	for attempt := 0; ; attempt++ {
		// Check context
		select {
		case <-ctx.Done():
			return nil, ErrContextCanceled
		default:
		}

		// Check max attempts
		if r.config.MaxAttempts > 0 && attempt >= r.config.MaxAttempts {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, ErrMaxAttemptsExceeded
		}

		// Execute function
		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Check if error is retryable
		if r.config.RetryableError != nil && !r.config.RetryableError(err) {
			return nil, err
		}

		// Calculate delay for next attempt
		if attempt == 0 {
			delay = r.config.InitialDelay
		} else {
			delay = r.calculateDelay(delay)
		}

		// Wait before retry
		select {
		case <-ctx.Done():
			return nil, ErrContextCanceled
		case <-time.After(delay):
		}
	}
}

// calculateDelay calculates the next delay with exponential backoff and jitter.
func (r *Retrier) calculateDelay(currentDelay time.Duration) time.Duration {
	// Apply multiplier
	newDelay := time.Duration(float64(currentDelay) * r.config.Multiplier)

	// Cap at max delay
	if newDelay > r.config.MaxDelay {
		newDelay = r.config.MaxDelay
	}

	// Apply jitter
	if r.config.Jitter > 0 {
		r.mu.Lock()
		jitter := r.rand.Float64() * float64(newDelay) * r.config.Jitter
		r.mu.Unlock()

		// Randomly add or subtract jitter
		if r.rand.Float64() < 0.5 {
			newDelay -= time.Duration(jitter)
		} else {
			newDelay += time.Duration(jitter)
		}
	}

	return newDelay
}

// Attempt represents a single retry attempt.
type Attempt struct {
	AttemptNumber int
	Delay         time.Duration
	Error         error
}

// DoWithCallback executes the given function with retry logic and calls the callback after each attempt.
func (r *Retrier) DoWithCallback(ctx context.Context, fn func() error, callback func(Attempt)) error {
	var lastErr error
	var delay time.Duration

	for attempt := 0; ; attempt++ {
		// Check context
		select {
		case <-ctx.Done():
			callback(Attempt{
				AttemptNumber: attempt,
				Delay:         0,
				Error:         ErrContextCanceled,
			})
			return ErrContextCanceled
		default:
		}

		// Check max attempts
		if r.config.MaxAttempts > 0 && attempt >= r.config.MaxAttempts {
			callback(Attempt{
				AttemptNumber: attempt,
				Delay:         0,
				Error:         lastErr,
			})
			if lastErr != nil {
				return lastErr
			}
			return ErrMaxAttemptsExceeded
		}

		// Execute function
		err := fn()
		if err == nil {
			callback(Attempt{
				AttemptNumber: attempt,
				Delay:         0,
				Error:         nil,
			})
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if r.config.RetryableError != nil && !r.config.RetryableError(err) {
			callback(Attempt{
				AttemptNumber: attempt,
				Delay:         0,
				Error:         err,
			})
			return err
		}

		// Calculate delay for next attempt
		if attempt == 0 {
			delay = r.config.InitialDelay
		} else {
			delay = r.calculateDelay(delay)
		}

		callback(Attempt{
			AttemptNumber: attempt,
			Delay:         delay,
			Error:         err,
		})

		// Wait before retry
		select {
		case <-ctx.Done():
			return ErrContextCanceled
		case <-time.After(delay):
		}
	}
}

// DoWithContext is a convenience method that creates a retrier with default config.
func DoWithContext(ctx context.Context, fn func() error) error {
	return NewRetrier(DefaultConfig()).Do(ctx, fn)
}

// Do is a convenience method that creates a retrier with default config and background context.
func Do(fn func() error) error {
	return DoWithContext(context.Background(), fn)
}

// WithMaxAttempts creates a new retrier with the specified max attempts.
func WithMaxAttempts(maxAttempts int) *Retrier {
	config := DefaultConfig()
	config.MaxAttempts = maxAttempts
	return NewRetrier(config)
}

// WithDelay creates a new retrier with the specified initial delay.
func WithDelay(delay time.Duration) *Retrier {
	config := DefaultConfig()
	config.InitialDelay = delay
	return NewRetrier(config)
}

// WithRetryableError creates a new retrier with a custom retryable error function.
func WithRetryableError(fn func(error) bool) *Retrier {
	config := DefaultConfig()
	config.RetryableError = fn
	return NewRetrier(config)
}
