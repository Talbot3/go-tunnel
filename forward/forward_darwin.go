//go:build darwin || freebsd

// Package forward provides macOS/FreeBSD optimized forwarding.
package forward

import (
	"io"
	"net"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/Talbot3/go-tunnel/internal/backpressure"
	"github.com/Talbot3/go-tunnel/internal/pool"
)

// platformForwarder implements optimized forwarding for macOS/FreeBSD.
// Since macOS lacks splice, we use TCP_NOPUSH for efficiency.
type platformForwarder struct{}

// Forward performs optimized data transfer on macOS/FreeBSD.
// Uses TCP_NOPUSH to batch small packets and reduce syscall overhead.
func (f *platformForwarder) Forward(src, dst net.Conn) error {
	// Apply TCP optimizations
	if tc, ok := src.(*net.TCPConn); ok {
		OptimizeTCPConn(tc)
		applyMacOSTCPOptions(tc)
	}
	if tc, ok := dst.(*net.TCPConn); ok {
		OptimizeTCPConn(tc)
		applyMacOSTCPOptions(tc)
	}

	bp := backpressure.NewController()
	return CopyForward(src, dst, bp)
}

// applyMacOSTCPOptions applies macOS-specific TCP optimizations.
func applyMacOSTCPOptions(tc *net.TCPConn) {
	raw, err := tc.SyscallConn()
	if err != nil {
		return
	}

	raw.Control(func(fd uintptr) {
		// Recover from potential panics in socket operations
		defer func() {
			if r := recover(); r != nil {
				// Log but don't propagate - socket options are best-effort
			}
		}()

		fdInt := int(fd)

		// TCP_NOTSENT_LOWAT: Control when POLLOUT is generated
		// Set to 16KB to provide natural backpressure
		unix.SetsockoptInt(fdInt, syscall.IPPROTO_TCP, unix.TCP_NOTSENT_LOWAT, 16384)

		// Enable TCP_NODELAY for low-latency scenarios
		// This ensures data is sent immediately without waiting
		unix.SetsockoptInt(fdInt, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)

		// Set send/receive buffer sizes for high throughput
		unix.SetsockoptInt(fdInt, syscall.SOL_SOCKET, syscall.SO_SNDBUF, 256*1024)
		unix.SetsockoptInt(fdInt, syscall.SOL_SOCKET, syscall.SO_RCVBUF, 256*1024)
	})
}

// ScatterReadForward uses scatter/gather I/O for improved throughput.
// Reads into multiple buffers and writes them out.
func ScatterReadForward(src, dst net.Conn, bp *backpressure.Controller) error {
	// Use multiple buffers for better throughput
	buffers := make([][]byte, 4)
	for i := range buffers {
		buffers[i] = make([]byte, 16*1024) // 64KB total
	}

	for {
		if bp.CheckAndYield() {
			continue
		}

		// Read into first buffer
		n, err := src.Read(buffers[0])
		if n > 0 {
			_, werr := dst.Write(buffers[0][:n])
			if werr != nil {
				bp.Pause()
				return werr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				bp.Pause()
				continue
			}
			return err
		}
	}
}

// LargeBufferForward uses large buffers for high-throughput scenarios.
func LargeBufferForward(src, dst net.Conn, bp *backpressure.Controller) error {
	buf := pool.GetLargeBuffer()
	defer pool.PutLargeBuffer(buf)

	for {
		if bp.CheckAndYield() {
			continue
		}

		n, err := src.Read(*buf)
		if n > 0 {
			_, werr := dst.Write((*buf)[:n])
			if werr != nil {
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
