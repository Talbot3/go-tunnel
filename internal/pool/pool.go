// Package pool provides buffer pool for high-performance I/O operations.
package pool

import "sync"

const (
	// DefaultBufferSize is the default buffer size (64KB)
	DefaultBufferSize = 64 * 1024
	// LargeBufferSize is for high-throughput scenarios (256KB)
	LargeBufferSize = 256 * 1024
)

// BufferPool manages reusable byte slices.
type BufferPool struct {
	pool *sync.Pool
	size int
}

// NewBufferPool creates a new buffer pool with specified buffer size.
func NewBufferPool(size int) *BufferPool {
	return &BufferPool{
		pool: &sync.Pool{
			New: func() interface{} {
				b := make([]byte, size)
				return &b
			},
		},
		size: size,
	}
}

// Get retrieves a buffer from the pool.
func (p *BufferPool) Get() *[]byte {
	return p.pool.Get().(*[]byte)
}

// Put returns a buffer to the pool.
func (p *BufferPool) Put(b *[]byte) {
	// Reset buffer size if it was modified
	if cap(*b) < p.size {
		*b = make([]byte, p.size)
	} else {
		*b = (*b)[:p.size]
	}
	p.pool.Put(b)
}

// Size returns the buffer size of this pool.
func (p *BufferPool) Size() int {
	return p.size
}

// Global pools for common use
var (
	// DefaultPool is a 64KB buffer pool
	DefaultPool = NewBufferPool(DefaultBufferSize)
	// LargePool is a 256KB buffer pool
	LargePool = NewBufferPool(LargeBufferSize)
)

// GetBuffer gets a buffer from the default pool.
func GetBuffer() *[]byte {
	return DefaultPool.Get()
}

// PutBuffer returns a buffer to the default pool.
func PutBuffer(b *[]byte) {
	DefaultPool.Put(b)
}

// GetLargeBuffer gets a large buffer from the large pool.
func GetLargeBuffer() *[]byte {
	return LargePool.Get()
}

// PutLargeBuffer returns a large buffer to the large pool.
func PutLargeBuffer(b *[]byte) {
	LargePool.Put(b)
}
