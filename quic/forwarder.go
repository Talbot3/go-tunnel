package quic

import (
	"context"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Talbot3/go-tunnel/internal/pool"
)

// BidirectionalForwarder handles bidirectional data forwarding between two endpoints.
// This eliminates duplicated code in forwardBidirectional, forwardQUICStream, forwardHTTP3Stream,
// handleTCPDataStreamForward, and handleQUICDataStreamForward.
type BidirectionalForwarder struct {
	ctx       context.Context
	cancel    context.CancelFunc
	reader    io.Reader
	writer    io.Writer
	bufIn     *[]byte
	bufOut    *[]byte
	bufferPool *pool.BufferPool

	// Callbacks
	onRead   func(n int)
	onWrite  func(n int)
	onDone   func()

	// Activity tracking
	activityTime *atomic.Int64

	// Wait group for goroutines
	wg sync.WaitGroup
}

// ForwarderConfig holds configuration for BidirectionalForwarder.
type ForwarderConfig struct {
	// BufferPool is the shared buffer pool.
	BufferPool *pool.BufferPool

	// StatsHandler for collecting statistics.
	StatsHandler StatsHandler

	// ActivityTime is the atomic timestamp for activity tracking.
	ActivityTime *atomic.Int64

	// OnRead is called after each read with the number of bytes read.
	OnRead func(n int)

	// OnWrite is called after each write with the number of bytes written.
	OnWrite func(n int)

	// OnDone is called when forwarding completes.
	OnDone func()
}

// NewBidirectionalForwarder creates a new bidirectional forwarder.
func NewBidirectionalForwarder(ctx context.Context, config ForwarderConfig) *BidirectionalForwarder {
	childCtx, cancel := context.WithCancel(ctx)

	f := &BidirectionalForwarder{
		ctx:         childCtx,
		cancel:      cancel,
		bufferPool:  config.BufferPool,
		activityTime: config.ActivityTime,
		onRead:      config.OnRead,
		onWrite:     config.OnWrite,
		onDone:      config.OnDone,
	}

	// Allocate buffers
	if f.bufferPool != nil {
		f.bufIn = f.bufferPool.Get()
		f.bufOut = f.bufferPool.Get()
	} else {
		defaultBuf := make([]byte, 32*1024)
		f.bufIn = &defaultBuf
		f.bufOut = &defaultBuf
	}

	return f
}

// Forward starts bidirectional forwarding between two endpoints.
// direction1: reader1 -> writer1 (data flows from reader1 to writer1)
// direction2: reader2 -> writer2 (data flows from reader2 to writer2)
func (f *BidirectionalForwarder) Forward(reader1 io.Reader, writer1 io.Writer, reader2 io.Reader, writer2 io.Writer) {
	// Direction 1: reader1 -> writer1
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		defer f.cancel() // Cancel the other direction on exit
		f.forwardDirection(reader1, writer1, f.bufOut, true)
	}()

	// Direction 2: reader2 -> writer2
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		defer f.cancel() // Cancel the other direction on exit
		f.forwardDirection(reader2, writer2, f.bufIn, false)
	}()

	// Wait for both directions
	f.wg.Wait()
}

// ForwardSingle starts single-direction forwarding.
// reader -> writer
func (f *BidirectionalForwarder) ForwardSingle(reader io.Reader, writer io.Writer) {
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		defer f.cancel()
		f.forwardDirection(reader, writer, f.bufIn, true)
	}()

	f.wg.Wait()
}

// forwardDirection forwards data from reader to writer.
func (f *BidirectionalForwarder) forwardDirection(reader io.Reader, writer io.Writer, buf *[]byte, isOutbound bool) {
	for {
		select {
		case <-f.ctx.Done():
			return
		default:
		}

		n, err := reader.Read(*buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("[Forwarder] Read error: %v", err)
			}
			return
		}

		if n == 0 {
			continue
		}

		// Write data
		written, err := writer.Write((*buf)[:n])
		if err != nil {
			if err != io.EOF {
				log.Printf("[Forwarder] Write error: %v", err)
			}
			return
		}

		// Update statistics
		if isOutbound {
			if f.onWrite != nil {
				f.onWrite(written)
			}
		} else {
			if f.onRead != nil {
				f.onRead(written)
			}
		}

		// Update activity time
		if f.activityTime != nil {
			f.activityTime.Store(time.Now().Unix())
		}
	}
}

// Close releases resources and calls the onDone callback.
func (f *BidirectionalForwarder) Close() {
	f.cancel()

	// Return buffers to pool
	if f.bufferPool != nil {
		if f.bufIn != nil {
			f.bufferPool.Put(f.bufIn)
		}
		if f.bufOut != nil {
			f.bufferPool.Put(f.bufOut)
		}
	}

	// Call onDone callback
	if f.onDone != nil {
		f.onDone()
	}
}

// RunBidirectionalForward runs bidirectional forwarding with cleanup.
// This is a convenience function that handles the full lifecycle.
func RunBidirectionalForward(
	ctx context.Context,
	endpoint1 io.ReadWriter,
	endpoint2 io.ReadWriter,
	config ForwarderConfig,
) {
	forwarder := NewBidirectionalForwarder(ctx, config)
	defer forwarder.Close()

	forwarder.Forward(endpoint1, endpoint1, endpoint2, endpoint2)
}

// ============================================
// Stream Forwarder (for QUIC streams)
// ============================================

// StreamForwarder handles forwarding between a QUIC stream and another endpoint.
type StreamForwarder struct {
	stream    interface {
		io.Reader
		io.Writer
		io.Closer
	}
	endpoint  io.ReadWriter
	bufferPool *pool.BufferPool
	onRead    func(n int)
	onWrite   func(n int)
}

// NewStreamForwarder creates a new stream forwarder.
func NewStreamForwarder(
	stream interface {
		io.Reader
		io.Writer
		io.Closer
	},
	endpoint io.ReadWriter,
	bufferPool *pool.BufferPool,
) *StreamForwarder {
	return &StreamForwarder{
		stream:     stream,
		endpoint:   endpoint,
		bufferPool: bufferPool,
	}
}

// Forward runs bidirectional forwarding.
func (f *StreamForwarder) Forward(ctx context.Context) error {
	forwarder := NewBidirectionalForwarder(ctx, ForwarderConfig{
		BufferPool: f.bufferPool,
		OnRead:     f.onRead,
		OnWrite:    f.onWrite,
	})
	defer forwarder.Close()

	forwarder.Forward(f.stream, f.stream, f.endpoint, f.endpoint)
	return nil
}

// ============================================
// Helper Functions
// ============================================

// forwardBidirectionalSimple is a simplified version for common use cases.
func forwardBidirectionalSimple(
	ctx context.Context,
	conn1 io.ReadWriter,
	conn2 io.ReadWriter,
	bufferPool *pool.BufferPool,
	onBytesIn func(n int),
	onBytesOut func(n int),
) {
	var bufIn, bufOut *[]byte
	if bufferPool != nil {
		bufIn = bufferPool.Get()
		bufOut = bufferPool.Get()
		defer bufferPool.Put(bufIn)
		defer bufferPool.Put(bufOut)
	} else {
		defaultBuf := make([]byte, 32*1024)
		bufIn = &defaultBuf
		bufOut = &defaultBuf
	}

	done := make(chan struct{}, 2)

	// conn1 -> conn2
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := conn1.Read(*bufIn)
			if err != nil {
				return
			}
			if _, err := conn2.Write((*bufIn)[:n]); err != nil {
				return
			}
			if onBytesOut != nil {
				onBytesOut(n)
			}
		}
	}()

	// conn2 -> conn1
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := conn2.Read(*bufOut)
			if err != nil {
				return
			}
			if _, err := conn1.Write((*bufOut)[:n]); err != nil {
				return
			}
			if onBytesIn != nil {
				onBytesIn(n)
			}
		}
	}()

	// Wait for both directions
	<-done
	<-done
}
