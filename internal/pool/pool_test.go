package pool

import (
	"sync"
	"testing"
)

func TestBufferPool_GetPut(t *testing.T) {
	pool := NewBufferPool(1024)

	// Get a buffer
	buf := pool.Get()
	if buf == nil {
		t.Fatal("expected non-nil buffer")
	}
	if len(*buf) != 1024 {
		t.Errorf("expected buffer size 1024, got %d", len(*buf))
	}
	if cap(*buf) != 1024 {
		t.Errorf("expected buffer capacity 1024, got %d", cap(*buf))
	}

	// Modify buffer
	(*buf)[0] = 42

	// Put it back
	pool.Put(buf)

	// Get again - should be reset
	buf2 := pool.Get()
	if len(*buf2) != 1024 {
		t.Errorf("expected buffer size 1024 after put, got %d", len(*buf2))
	}
	// Note: content may or may not be preserved
	pool.Put(buf2)
}

func TestBufferPool_Concurrent(t *testing.T) {
	pool := NewBufferPool(4096)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := pool.Get()
			if len(*buf) != 4096 {
				t.Errorf("expected buffer size 4096, got %d", len(*buf))
			}
			// Simulate work
			for j := 0; j < 100; j++ {
				(*buf)[j] = byte(j)
			}
			pool.Put(buf)
		}()
	}

	wg.Wait()
}

func TestBufferPool_Size(t *testing.T) {
	pool := NewBufferPool(2048)
	if pool.Size() != 2048 {
		t.Errorf("expected size 2048, got %d", pool.Size())
	}
}

func TestGlobalPools(t *testing.T) {
	// Test small buffer
	smallBuf := GetSmallBuffer()
	if len(*smallBuf) != SmallBufferSize {
		t.Errorf("expected small buffer size %d, got %d", SmallBufferSize, len(*smallBuf))
	}
	PutSmallBuffer(smallBuf)

	// Test default buffer
	defaultBuf := GetBuffer()
	if len(*defaultBuf) != DefaultBufferSize {
		t.Errorf("expected default buffer size %d, got %d", DefaultBufferSize, len(*defaultBuf))
	}
	PutBuffer(defaultBuf)

	// Test large buffer
	largeBuf := GetLargeBuffer()
	if len(*largeBuf) != LargeBufferSize {
		t.Errorf("expected large buffer size %d, got %d", LargeBufferSize, len(*largeBuf))
	}
	PutLargeBuffer(largeBuf)

	// Test xlarge buffer
	xlargeBuf := GetXLargeBuffer()
	if len(*xlargeBuf) != XLargeBufferSize {
		t.Errorf("expected xlarge buffer size %d, got %d", XLargeBufferSize, len(*xlargeBuf))
	}
	PutXLargeBuffer(xlargeBuf)
}

func TestGetBufferForSize(t *testing.T) {
	tests := []struct {
		size         int
		expectedSize int
	}{
		{100, SmallBufferSize},
		{SmallBufferSize, SmallBufferSize},
		{SmallBufferSize + 1, DefaultBufferSize},
		{DefaultBufferSize, DefaultBufferSize},
		{DefaultBufferSize + 1, LargeBufferSize},
		{LargeBufferSize, LargeBufferSize},
		{LargeBufferSize + 1, XLargeBufferSize},
		{XLargeBufferSize + 1, XLargeBufferSize},
	}

	for _, tt := range tests {
		buf := GetBufferForSize(tt.size)
		if len(*buf) != tt.expectedSize {
			t.Errorf("GetBufferForSize(%d): expected size %d, got %d", tt.size, tt.expectedSize, len(*buf))
		}
		PutBufferForSize(buf)
	}
}

func TestPutBufferForSize(t *testing.T) {
	// Create buffers of different capacities
	smallBuf := GetSmallBuffer()
	defaultBuf := GetBuffer()
	largeBuf := GetLargeBuffer()
	xlargeBuf := GetXLargeBuffer()

	// Put them back using PutBufferForSize
	PutBufferForSize(smallBuf)
	PutBufferForSize(defaultBuf)
	PutBufferForSize(largeBuf)
	PutBufferForSize(xlargeBuf)

	// Verify they can be retrieved again
	smallBuf2 := GetSmallBuffer()
	defaultBuf2 := GetBuffer()
	largeBuf2 := GetLargeBuffer()
	xlargeBuf2 := GetXLargeBuffer()

	if len(*smallBuf2) != SmallBufferSize {
		t.Error("small buffer retrieval failed")
	}
	if len(*defaultBuf2) != DefaultBufferSize {
		t.Error("default buffer retrieval failed")
	}
	if len(*largeBuf2) != LargeBufferSize {
		t.Error("large buffer retrieval failed")
	}
	if len(*xlargeBuf2) != XLargeBufferSize {
		t.Error("xlarge buffer retrieval failed")
	}

	// Clean up
	PutSmallBuffer(smallBuf2)
	PutBuffer(defaultBuf2)
	PutLargeBuffer(largeBuf2)
	PutXLargeBuffer(xlargeBuf2)
}

func TestBufferPool_ResizeOnPut(t *testing.T) {
	pool := NewBufferPool(1024)

	// Get a buffer
	buf := pool.Get()

	// Shrink the buffer slice
	*buf = (*buf)[:100]

	// Put it back - should be reset to original size
	pool.Put(buf)

	// Get again
	buf2 := pool.Get()
	if len(*buf2) != 1024 {
		t.Errorf("expected buffer to be reset to 1024, got %d", len(*buf2))
	}
	pool.Put(buf2)
}
