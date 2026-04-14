package tunnel

import "errors"

// Errors returned by the tunnel package.
var (
	// ErrUnknownProtocol is returned when an unknown protocol is specified.
	ErrUnknownProtocol = errors.New("unknown protocol")

	// ErrConnectionClosed is returned when a connection is closed.
	ErrConnectionClosed = errors.New("connection closed")

	// ErrTimeout is returned when an operation times out.
	ErrTimeout = errors.New("operation timeout")

	// ErrBufferTooSmall is returned when the buffer is too small.
	ErrBufferTooSmall = errors.New("buffer too small")

	// ErrConnectionLimit is returned when connection limit is reached.
	ErrConnectionLimit = errors.New("connection limit reached")

	// ErrConnectionIdle is returned when a connection is idle for too long.
	ErrConnectionIdle = errors.New("connection idle timeout")

	// ErrPoolExhausted is returned when the connection pool is exhausted.
	ErrPoolExhausted = errors.New("connection pool exhausted")
)
