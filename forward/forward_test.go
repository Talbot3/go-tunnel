package forward

import (
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Talbot3/go-tunnel/internal/backpressure"
)

// mockConn is a mock net.Conn for testing
type mockConn struct {
	readData  []byte
	readPos   int
	writeData []byte
	readErr   error
	writeErr  error
	closed    atomic.Bool
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	if m.readErr != nil {
		return 0, m.readErr
	}
	if m.readPos >= len(m.readData) {
		return 0, io.EOF
	}
	n = copy(b, m.readData[m.readPos:])
	m.readPos += n
	return n, nil
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	m.writeData = append(m.writeData, b...)
	return len(b), nil
}

func (m *mockConn) Close() error {
	m.closed.Store(true)
	return nil
}

func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func TestNewForwarder(t *testing.T) {
	fwd := NewForwarder()
	if fwd == nil {
		t.Fatal("expected non-nil forwarder")
	}
}

func TestForward_Success(t *testing.T) {
	fwd := NewForwarder()

	src := &mockConn{readData: []byte("hello world")}
	dst := &mockConn{}

	err := fwd.Forward(src, dst)
	if err != nil {
		t.Errorf("Forward failed: %v", err)
	}

	if string(dst.writeData) != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", string(dst.writeData))
	}
}

func TestForward_Empty(t *testing.T) {
	fwd := NewForwarder()

	src := &mockConn{readData: []byte{}}
	dst := &mockConn{}

	err := fwd.Forward(src, dst)
	if err != nil {
		t.Errorf("Forward failed: %v", err)
	}

	if len(dst.writeData) != 0 {
		t.Errorf("expected no data, got '%s'", string(dst.writeData))
	}
}

func TestForward_ReadError(t *testing.T) {
	fwd := NewForwarder()

	testErr := errors.New("read error")
	src := &mockConn{readErr: testErr}
	dst := &mockConn{}

	err := fwd.Forward(src, dst)
	if err != testErr {
		t.Errorf("expected read error, got %v", err)
	}
}

func TestForward_WriteError(t *testing.T) {
	fwd := NewForwarder()

	testErr := errors.New("write error")
	src := &mockConn{readData: []byte("hello")}
	dst := &mockConn{writeErr: testErr}

	err := fwd.Forward(src, dst)
	if err != testErr {
		t.Errorf("expected write error, got %v", err)
	}
}

func TestIsClosedErr_Nil(t *testing.T) {
	if !IsClosedErr(nil) {
		t.Error("expected nil to be a closed error")
	}
}

func TestIsClosedErr_EOF(t *testing.T) {
	if !IsClosedErr(io.EOF) {
		t.Error("expected EOF to be a closed error")
	}
}

func TestIsClosedErr_Syscall(t *testing.T) {
	errs := []error{syscall.EPIPE, syscall.ECONNRESET, syscall.ECONNABORTED}
	for _, err := range errs {
		if !IsClosedErr(err) {
			t.Errorf("expected %v to be a closed error", err)
		}
	}
}

func TestIsClosedErr_Other(t *testing.T) {
	err := errors.New("some other error")
	if IsClosedErr(err) {
		t.Error("expected other error to not be a closed error")
	}
}

func TestCopyForward_Success(t *testing.T) {
	src := &mockConn{readData: []byte("test data")}
	dst := &mockConn{}
	bp := backpressure.NewController()

	err := CopyForward(src, dst, bp)
	if err != nil {
		t.Errorf("CopyForward failed: %v", err)
	}

	if string(dst.writeData) != "test data" {
		t.Errorf("expected 'test data', got '%s'", string(dst.writeData))
	}
}

func TestCopyForward_WithBackpressure(t *testing.T) {
	src := &mockConn{readData: []byte("test")}
	dst := &mockConn{}
	bp := backpressure.NewController()

	// Pause and then resume
	bp.Pause()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		bp.Resume()
	}()

	err := CopyForward(src, dst, bp)
	wg.Wait()

	if err != nil {
		t.Errorf("CopyForward failed: %v", err)
	}
}

func TestBufferForward_Success(t *testing.T) {
	src := &mockConn{readData: []byte("test data")}
	dst := &mockConn{}
	bp := backpressure.NewController()

	err := BufferForward(src, dst, bp, 64*1024)
	if err != nil {
		t.Errorf("BufferForward failed: %v", err)
	}

	if string(dst.writeData) != "test data" {
		t.Errorf("expected 'test data', got '%s'", string(dst.writeData))
	}
}

func TestBufferForward_LargeBuffer(t *testing.T) {
	src := &mockConn{readData: []byte("test data")}
	dst := &mockConn{}
	bp := backpressure.NewController()

	err := BufferForward(src, dst, bp, 256*1024) // Large buffer
	if err != nil {
		t.Errorf("BufferForward failed: %v", err)
	}

	if string(dst.writeData) != "test data" {
		t.Errorf("expected 'test data', got '%s'", string(dst.writeData))
	}
}

func TestHandlePair(t *testing.T) {
	// Create a pipe for bidirectional communication
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		HandlePair(server, server)
	}()

	// Give time for the goroutines to start
	time.Sleep(10 * time.Millisecond)

	// Close client to trigger EOF
	client.Close()

	wg.Wait()
}

func TestOptimizeTCPConn_Nil(t *testing.T) {
	err := OptimizeTCPConn(nil)
	if err != nil {
		t.Errorf("OptimizeTCPConn(nil) should return nil, got %v", err)
	}
}

func TestForward_LargeData(t *testing.T) {
	fwd := NewForwarder()

	// Create large data (1MB)
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	src := &mockConn{readData: data}
	dst := &mockConn{}

	err := fwd.Forward(src, dst)
	if err != nil {
		t.Errorf("Forward failed: %v", err)
	}

	if len(dst.writeData) != len(data) {
		t.Errorf("expected %d bytes, got %d", len(data), len(dst.writeData))
	}

	// Verify data integrity
	for i := range data {
		if dst.writeData[i] != data[i] {
			t.Errorf("data mismatch at position %d", i)
			break
		}
	}
}

func TestForward_MultipleReads(t *testing.T) {
	fwd := NewForwarder()

	// Create data that will require multiple reads
	data := make([]byte, 200*1024) // 200KB
	for i := range data {
		data[i] = byte(i % 256)
	}

	src := &mockConn{readData: data}
	dst := &mockConn{}

	err := fwd.Forward(src, dst)
	if err != nil {
		t.Errorf("Forward failed: %v", err)
	}

	if len(dst.writeData) != len(data) {
		t.Errorf("expected %d bytes, got %d", len(data), len(dst.writeData))
	}
}

// Benchmark tests
func BenchmarkForward(b *testing.B) {
	fwd := NewForwarder()
	data := make([]byte, 64*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := &mockConn{readData: data}
		dst := &mockConn{}
		fwd.Forward(src, dst)
	}
}

func BenchmarkCopyForward(b *testing.B) {
	data := make([]byte, 64*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	bp := backpressure.NewController()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := &mockConn{readData: data}
		dst := &mockConn{}
		CopyForward(src, dst, bp)
	}
}

func BenchmarkBufferForward(b *testing.B) {
	data := make([]byte, 64*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	bp := backpressure.NewController()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := &mockConn{readData: data}
		dst := &mockConn{}
		BufferForward(src, dst, bp, 64*1024)
	}
}
