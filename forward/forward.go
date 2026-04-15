// Package forward provides platform-optimized data forwarding.
package forward

import (
	"io"
	"net"
	"sync"
	"syscall"

	"github.com/Talbot3/go-tunnel/internal/backpressure"
	"github.com/Talbot3/go-tunnel/internal/pool"
)

// Forwarder defines the interface for data forwarding.
type Forwarder interface {
	// Forward copies data from src to dst.
	Forward(src, dst net.Conn) error
}

// NewForwarder creates a platform-optimized forwarder.
// The actual implementation is determined by build constraints:
// - Linux: uses unix.Splice for zero-copy
// - macOS/FreeBSD: uses TCP_NOPUSH optimization
// - Windows: uses IOCP with large buffers
func NewForwarder() Forwarder {
	return &platformForwarder{}
}

// IsClosedErr returns true if the error indicates a normal connection close.
// Returns false for nil error (no error means not a "closed" error).
func IsClosedErr(err error) bool {
	if err == nil {
		return false // No error, not a "closed" error
	}
	if err == io.EOF {
		return true
	}
	// Check for common connection close errors
	switch err {
	case syscall.EPIPE, syscall.ECONNRESET, syscall.ECONNABORTED:
		return true
	}
	// Check for net.OpError
	if opErr, ok := err.(*net.OpError); ok {
		// Check for "use of closed network connection"
		if opErr.Err.Error() == "use of closed network connection" {
			return true
		}
		// Check for syscall errors wrapped in OpError
		switch opErr.Err {
		case syscall.EPIPE, syscall.ECONNRESET, syscall.ECONNABORTED:
			return true
		}
	}
	return false
}

// HandlePair starts bidirectional forwarding between two connections.
func HandlePair(connA, connB net.Conn) {
	fwd := NewForwarder()
	var wg sync.WaitGroup
	wg.Add(2)

	bp := backpressure.NewPair()

	go func() {
		defer wg.Done()
		if err := fwd.Forward(connA, connB); err != nil {
			bp.SignalErrorA()
		}
	}()

	go func() {
		defer wg.Done()
		if err := fwd.Forward(connB, connA); err != nil {
			bp.SignalErrorB()
		}
	}()

	wg.Wait()
	connA.Close()
	connB.Close()
}

// CopyForward is a generic forward implementation using buffer pool.
// Used as fallback for platforms without special optimizations.
func CopyForward(src, dst net.Conn, bp *backpressure.Controller) error {
	buf := pool.GetBuffer()
	defer pool.PutBuffer(buf)

	for {
		if bp.CheckAndYield() {
			continue
		}

		n, err := src.Read(*buf)
		if n > 0 {
			if _, werr := dst.Write((*buf)[:n]); werr != nil {
				bp.Pause()
				return werr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// BufferForward forwards data using buffer pool with backpressure control.
func BufferForward(src, dst net.Conn, bp *backpressure.Controller, bufSize int) error {
	var buf *[]byte
	if bufSize >= pool.LargeBufferSize {
		buf = pool.GetLargeBuffer()
		defer pool.PutLargeBuffer(buf)
	} else {
		buf = pool.GetBuffer()
		defer pool.PutBuffer(buf)
	}

	for {
		if bp.CheckAndYield() {
			continue
		}

		n, err := src.Read(*buf)
		if n > 0 {
			if _, werr := dst.Write((*buf)[:n]); werr != nil {
				bp.Pause()
				return werr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// OptimizeTCPConn applies common TCP optimizations.
func OptimizeTCPConn(conn *net.TCPConn) error {
	if conn == nil {
		return nil
	}
	// Disable Nagle's algorithm for lower latency
	if err := conn.SetNoDelay(true); err != nil {
		return err
	}
	// Set keep-alive for connection health
	if err := conn.SetKeepAlive(true); err != nil {
		return err
	}
	// 30 seconds keep-alive period
	return conn.SetKeepAlivePeriod(30e9)
}
