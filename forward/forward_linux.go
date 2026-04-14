//go:build linux

// Package forward provides Linux-specific zero-copy forwarding using splice.
package forward

import (
	"errors"
	"io"
	"net"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Talbot3/go-tunnel/internal/backpressure"
	"github.com/Talbot3/go-tunnel/internal/pool"
)

// platformForwarder implements zero-copy forwarding on Linux using splice.
type platformForwarder struct{}

// Forward performs zero-copy data transfer using unix.Splice.
// This eliminates kernel-to-userspace memory copies.
func (f *platformForwarder) Forward(src, dst net.Conn) error {
	// Apply TCP optimizations
	if tc, ok := src.(*net.TCPConn); ok {
		OptimizeTCPConn(tc)
	}
	if tc, ok := dst.(*net.TCPConn); ok {
		OptimizeTCPConn(tc)
	}

	// Extract raw file descriptors for splice
	srcFd, err := getFd(src)
	if err != nil {
		// Fallback to buffered copy if fd extraction fails
		bp := backpressure.NewController()
		return BufferForward(src, dst, bp, pool.DefaultBufferSize)
	}

	dstFd, err := getFd(dst)
	if err != nil {
		bp := backpressure.NewController()
		return BufferForward(src, dst, bp, pool.DefaultBufferSize)
	}

	// Use splice for zero-copy transfer
	return spliceForward(srcFd, dstFd)
}

// getFd extracts the file descriptor from a connection.
func getFd(conn net.Conn) (int, error) {
	sc, ok := conn.(syscall.Conn)
	if !ok {
		return -1, errors.New("not a syscall.Conn")
	}

	raw, err := sc.SyscallConn()
	if err != nil {
		return -1, err
	}

	var fd int
	var controlErr error
	raw.Control(func(fdPtr uintptr) {
		defer func() {
			if r := recover(); r != nil {
				controlErr = errors.New("control function panicked")
			}
		}()
		fd = int(fdPtr)
	})
	if controlErr != nil {
		return -1, controlErr
	}
	return fd, nil
}

// spliceForward performs zero-copy transfer using splice syscall.
func spliceForward(srcFd, dstFd int) error {
	bp := backpressure.NewController()

	const (
		spliceSize = 64 * 1024 // 64KB per splice call
		flags      = unix.SPLICE_F_NONBLOCK | unix.SPLICE_F_MORE
	)

	for {
		if bp.CheckAndYield() {
			continue
		}

		n, err := unix.Splice(srcFd, nil, dstFd, nil, spliceSize, flags)

		if n > 0 {
			// Successfully transferred data, continue
			continue
		}

		if err != nil {
			// Handle EAGAIN/EWOULDBLOCK - kernel buffer full
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				bp.Pause()
				// Wait for kernel TCP window to recover
				time.Sleep(50 * time.Microsecond)
				continue
			}

			// Check for connection closed
			if errors.Is(err, io.EOF) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
				return nil
			}

			return err
		}

		// n == 0 && err == nil means EOF
		if n == 0 {
			return nil
		}
	}
}

// SpliceFileToFile copies data between two file descriptors using splice.
// This is a lower-level API for advanced use cases.
func SpliceFileToFile(srcFd, dstFd int, size int64) (int64, error) {
	var total int64
	const chunkSize = 64 * 1024

	for total < size {
		toTransfer := chunkSize
		if remaining := size - total; remaining < chunkSize {
			toTransfer = int(remaining)
		}

		n, err := unix.Splice(srcFd, nil, dstFd, nil, toTransfer, unix.SPLICE_F_MORE)
		if n > 0 {
			total += int64(n)
		}
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				time.Sleep(50 * time.Microsecond)
				continue
			}
			return total, err
		}
		if n == 0 {
			break
		}
	}

	return total, nil
}

// Tee duplicates data from src to both dst1 and dst2 using splice tee.
// Useful for monitoring/logging without extra memory copies.
func Tee(srcFd, dst1Fd, dst2Fd int, size int64) (int64, error) {
	var total int64
	const chunkSize = 64 * 1024

	for total < size {
		toTransfer := chunkSize
		if remaining := size - total; remaining < chunkSize {
			toTransfer = int(remaining)
		}

		// First, tee to dst2 (duplicates data without consuming)
		n, err := unix.Tee(srcFd, dst2Fd, toTransfer, unix.SPLICE_F_NONBLOCK)
		if n > 0 {
			// Then splice to dst1 (consumes data)
			spliced, err := unix.Splice(srcFd, nil, dst1Fd, nil, int(n), unix.SPLICE_F_MORE)
			if spliced > 0 {
				total += int64(spliced)
			}
			if err != nil {
				return total, err
			}
		}
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				time.Sleep(50 * time.Microsecond)
				continue
			}
			return total, err
		}
		if n == 0 {
			break
		}
	}

	return total, nil
}
