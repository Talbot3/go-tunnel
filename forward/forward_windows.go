//go:build windows

// Package forward provides Windows optimized forwarding using IOCP.
package forward

import (
	"io"
	"net"
	"syscall"

	"github.com/Talbot3/go-tunnel/internal/backpressure"
	"github.com/Talbot3/go-tunnel/internal/pool"
)

// Windows socket option constants not defined in syscall package.
const (
	// TCP_FASTOPEN enables TCP Fast Open (Windows 10+).
	// This allows data to be sent in the SYN packet.
	TCP_FASTOPEN = 23
)

// platformForwarder implements optimized forwarding for Windows.
// Windows uses IOCP (I/O Completion Ports) underneath Go's net package.
type platformForwarder struct{}

// Forward performs optimized data transfer on Windows.
// Uses large buffers and relies on Go's IOCP implementation.
func (f *platformForwarder) Forward(src, dst net.Conn) error {
	// Apply TCP optimizations
	if tc, ok := src.(*net.TCPConn); ok {
		OptimizeTCPConn(tc)
		applyWindowsTCPOptions(tc)
	}
	if tc, ok := dst.(*net.TCPConn); ok {
		OptimizeTCPConn(tc)
		applyWindowsTCPOptions(tc)
	}

	bp := backpressure.NewController()
	// Use large buffer for better IOCP performance
	return BufferForward(src, dst, bp, pool.LargeBufferSize)
}

// applyWindowsTCPOptions applies Windows-specific TCP optimizations.
func applyWindowsTCPOptions(tc *net.TCPConn) {
	// Set larger buffer sizes for better throughput with IOCP
	tc.SetReadBuffer(256 * 1024)
	tc.SetWriteBuffer(256 * 1024)

	// Get raw connection for socket options
	raw, err := tc.SyscallConn()
	if err != nil {
		return
	}

	raw.Control(func(fd uintptr) {
		// Enable TCP_NODELAY for lower latency
		syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)

		// Enable TCP_FASTOPEN if available (Windows 10+)
		// This allows data to be sent in the SYN packet
		syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_TCP, TCP_FASTOPEN, 1)

		// Set SO_REUSEADDR for faster port reuse
		syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	})
}

// IOCPForward uses Windows IOCP for overlapped I/O operations.
// Go's net package already uses IOCP internally, so we optimize buffer usage.
func IOCPForward(src, dst net.Conn, bp *backpressure.Controller) error {
	// Use multiple buffers for pipelining
	buffers := make([][]byte, 2)
	for i := range buffers {
		buffers[i] = make([]byte, pool.LargeBufferSize)
	}

	// Alternate between buffers for read/write pipelining
	bufIdx := 0
	pending := false
	pendingN := 0

	for {
		if bp.CheckAndYield() {
			continue
		}

		buf := buffers[bufIdx]

		if pending {
			// Write previous read
			_, werr := dst.Write(buf[:pendingN])
			if werr != nil {
				bp.Pause()
				return werr
			}
			pending = false
		}

		// Read new data
		n, err := src.Read(buf)
		if n > 0 {
			pendingN = n
			pending = true
			bufIdx = (bufIdx + 1) % len(buffers)
		}

		if err != nil {
			if err == io.EOF {
				// Flush pending write
				if pending {
					dst.Write(buffers[(bufIdx+1)%2][:pendingN])
				}
				return nil
			}
			return err
		}
	}
}

// ScatterGatherForward uses scatter/gather I/O patterns.
// This is optimized for Windows overlapped I/O.
func ScatterGatherForward(src, dst net.Conn, bp *backpressure.Controller) error {
	// Use net.Buffers for writev-like behavior
	var buffers net.Buffers
	buf := pool.GetLargeBuffer()
	defer pool.PutLargeBuffer(buf)

	for {
		if bp.CheckAndYield() {
			continue
		}

		n, err := src.Read(*buf)
		if n > 0 {
			// Add to buffers for scatter/gather write
			buffers = append(buffers, (*buf)[:n])

			// Write all buffers at once (uses WSASend internally)
			_, werr := buffers.WriteTo(dst)
			if werr != nil {
				bp.Pause()
				return werr
			}
			buffers = buffers[:0] // Clear buffers
		}

		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}
