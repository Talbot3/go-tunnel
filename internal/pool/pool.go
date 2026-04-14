// Package pool provides buffer pool for high-performance I/O operations.
package pool

import "sync"

const (
	// SmallBufferSize is for lightweight connections (8KB)
	SmallBufferSize = 8 * 1024
	// DefaultBufferSize is the default buffer size (64KB)
	DefaultBufferSize = 64 * 1024
	// LargeBufferSize is for high-throughput scenarios (256KB)
	LargeBufferSize = 256 * 1024
	// XLargeBufferSize is for large response scenarios (512KB)
	XLargeBufferSize = 512 * 1024
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
			New: func() any {
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
	// SmallPool is a 8KB buffer pool for lightweight connections
	SmallPool = NewBufferPool(SmallBufferSize)
	// DefaultPool is a 64KB buffer pool
	DefaultPool = NewBufferPool(DefaultBufferSize)
	// LargePool is a 256KB buffer pool
	LargePool = NewBufferPool(LargeBufferSize)
	// XLargePool is a 512KB buffer pool for large responses
	XLargePool = NewBufferPool(XLargeBufferSize)
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

// GetSmallBuffer gets a small buffer from the small pool.
func GetSmallBuffer() *[]byte {
	return SmallPool.Get()
}

// PutSmallBuffer returns a small buffer to the small pool.
func PutSmallBuffer(b *[]byte) {
	SmallPool.Put(b)
}

// GetXLargeBuffer gets an extra large buffer from the xlarge pool.
func GetXLargeBuffer() *[]byte {
	return XLargePool.Get()
}

// PutXLargeBuffer returns an extra large buffer to the xlarge pool.
func PutXLargeBuffer(b *[]byte) {
	XLargePool.Put(b)
}

// GetBufferForSize returns a buffer from the appropriate pool based on size.
// This allows dynamic buffer selection based on expected data size.
func GetBufferForSize(size int) *[]byte {
	switch {
	case size <= SmallBufferSize:
		return SmallPool.Get()
	case size <= DefaultBufferSize:
		return DefaultPool.Get()
	case size <= LargeBufferSize:
		return LargePool.Get()
	default:
		return XLargePool.Get()
	}
}

// PutBufferForSize returns a buffer to the appropriate pool based on its capacity.
func PutBufferForSize(b *[]byte) {
	c := cap(*b)
	switch {
	case c <= SmallBufferSize:
		SmallPool.Put(b)
	case c <= DefaultBufferSize:
		DefaultPool.Put(b)
	case c <= LargeBufferSize:
		LargePool.Put(b)
	default:
		XLargePool.Put(b)
	}
}
