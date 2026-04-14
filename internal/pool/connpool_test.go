package pool

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockConn is a mock net.Conn for testing
type mockConn struct {
	closed *atomic.Bool
}

func newMockConn() *mockConn {
	return &mockConn{closed: &atomic.Bool{}}
}

func (m *mockConn) Read(b []byte) (n int, err error)  { return 0, errors.New("not implemented") }
func (m *mockConn) Write(b []byte) (n int, err error) { return len(b), nil }
func (m *mockConn) Close() error {
	m.closed.Store(true)
	return nil
}
func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }
func (m *mockConn) IsClosed() bool                     { return m.closed.Load() }

func TestConnPool_Get_New(t *testing.T) {
	var created atomic.Int32
	factory := func() (net.Conn, error) {
		created.Add(1)
		return newMockConn(), nil
	}

	cfg := DefaultConnPoolConfig()
	pool := NewConnPool(factory, cfg)

	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if conn == nil {
		t.Fatal("expected non-nil connection")
	}
	if created.Load() != 1 {
		t.Errorf("expected 1 connection created, got %d", created.Load())
	}

	conn.Close()
	pool.Close()
}

func TestConnPool_Get_Reuse(t *testing.T) {
	var created atomic.Int32
	factory := func() (net.Conn, error) {
		created.Add(1)
		return newMockConn(), nil
	}

	cfg := DefaultConnPoolConfig()
	cfg.MaxIdle = 5
	pool := NewConnPool(factory, cfg)

	ctx := context.Background()

	// Get and put back 3 times
	for i := 0; i < 3; i++ {
		conn, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		conn.Close() // Returns to pool
	}

	// Should have created only 1 connection
	if created.Load() != 1 {
		t.Errorf("expected 1 connection created, got %d", created.Load())
	}

	stats := pool.GetStats()
	if stats.Reused != 2 {
		t.Errorf("expected 2 reused connections, got %d", stats.Reused)
	}

	pool.Close()
}

func TestConnPool_MaxIdle(t *testing.T) {
	var created atomic.Int32
	factory := func() (net.Conn, error) {
		created.Add(1)
		return newMockConn(), nil
	}

	cfg := DefaultConnPoolConfig()
	cfg.MaxIdle = 2
	pool := NewConnPool(factory, cfg)

	ctx := context.Background()

	// Create 5 connections
	var conns []net.Conn
	for i := 0; i < 5; i++ {
		conn, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		conns = append(conns, conn)
	}

	// Return all to pool
	for _, conn := range conns {
		conn.Close()
	}

	stats := pool.GetStats()
	// Only MaxIdle connections should be kept
	if stats.Idle > cfg.MaxIdle {
		t.Errorf("expected at most %d idle connections, got %d", cfg.MaxIdle, stats.Idle)
	}

	pool.Close()
}

func TestConnPool_MaxAge(t *testing.T) {
	var created atomic.Int32
	factory := func() (net.Conn, error) {
		created.Add(1)
		return newMockConn(), nil
	}

	cfg := DefaultConnPoolConfig()
	cfg.MaxAge = 100 * time.Millisecond
	pool := NewConnPool(factory, cfg)

	ctx := context.Background()

	// Get a connection
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// Wait for it to expire
	time.Sleep(150 * time.Millisecond)

	// Return to pool
	conn.Close()

	// Get again - should create new connection due to age
	conn2, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	conn2.Close()

	// Should have created 2 connections (first expired)
	if created.Load() != 2 {
		t.Errorf("expected 2 connections created, got %d", created.Load())
	}

	pool.Close()
}

func TestConnPool_Close(t *testing.T) {
	factory := func() (net.Conn, error) {
		return newMockConn(), nil
	}

	cfg := DefaultConnPoolConfig()
	cfg.MaxIdle = 5
	pool := NewConnPool(factory, cfg)

	ctx := context.Background()

	// Create 3 connections and keep them (don't return to pool yet)
	var conns []net.Conn
	for i := 0; i < 3; i++ {
		conn, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		conns = append(conns, conn)
	}

	// Return all to pool
	for _, conn := range conns {
		conn.Close()
	}

	// Verify idle connections
	stats := pool.GetStats()
	if stats.Idle != 3 {
		t.Errorf("expected 3 idle connections, got %d", stats.Idle)
	}

	// Close pool
	err := pool.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify pool is closed
	_, err = pool.Get(ctx)
	if err != ErrPoolClosed {
		t.Errorf("expected ErrPoolClosed, got %v", err)
	}

	// After closing, all idle connections should be closed
	stats = pool.GetStats()
	if int(stats.Closed) != 3 {
		t.Errorf("expected 3 closed connections, got %d", stats.Closed)
	}
}

func TestConnPool_Prune(t *testing.T) {
	var created atomic.Int32
	factory := func() (net.Conn, error) {
		created.Add(1)
		return newMockConn(), nil
	}

	cfg := DefaultConnPoolConfig()
	cfg.MaxAge = 100 * time.Millisecond
	cfg.MaxIdle = 10
	pool := NewConnPool(factory, cfg)

	ctx := context.Background()

	// Create 5 connections
	var conns []net.Conn
	for i := 0; i < 5; i++ {
		conn, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		conns = append(conns, conn)
	}

	// Return all to pool
	for _, conn := range conns {
		conn.Close()
	}

	// Wait for connections to expire
	time.Sleep(150 * time.Millisecond)

	// Prune expired connections
	pruned := pool.Prune()
	if pruned != 5 {
		t.Errorf("expected 5 pruned connections, got %d", pruned)
	}

	stats := pool.GetStats()
	if stats.Idle != 0 {
		t.Errorf("expected 0 idle connections after prune, got %d", stats.Idle)
	}

	pool.Close()
}

func TestConnPool_Concurrent(t *testing.T) {
	var created atomic.Int32
	factory := func() (net.Conn, error) {
		created.Add(1)
		return newMockConn(), nil
	}

	cfg := DefaultConnPoolConfig()
	cfg.MaxIdle = 10
	pool := NewConnPool(factory, cfg)

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent get/put operations
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := pool.Get(ctx)
			if err != nil {
				t.Errorf("Get failed: %v", err)
				return
			}
			// Simulate work
			time.Sleep(time.Microsecond)
			conn.Close()
		}()
	}

	wg.Wait()

	stats := pool.GetStats()
	if stats.Created+stats.Reused != 100 {
		t.Errorf("expected 100 total operations, got created=%d reused=%d", stats.Created, stats.Reused)
	}

	pool.Close()
}

func TestConnPool_ContextCancel(t *testing.T) {
	factory := func() (net.Conn, error) {
		time.Sleep(100 * time.Millisecond) // Slow factory
		return newMockConn(), nil
	}

	cfg := DefaultConnPoolConfig()
	pool := NewConnPool(factory, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := pool.Get(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	pool.Close()
}

func TestConnPool_Stats(t *testing.T) {
	factory := func() (net.Conn, error) {
		return newMockConn(), nil
	}

	cfg := DefaultConnPoolConfig()
	cfg.MaxIdle = 5
	pool := NewConnPool(factory, cfg)

	ctx := context.Background()

	// Create 3 connections
	var conns []net.Conn
	for i := 0; i < 3; i++ {
		conn, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		conns = append(conns, conn)
	}

	stats := pool.GetStats()
	if stats.Created != 3 {
		t.Errorf("expected 3 created, got %d", stats.Created)
	}
	if stats.MaxIdle != 5 {
		t.Errorf("expected MaxIdle 5, got %d", stats.MaxIdle)
	}

	// Return connections
	for _, conn := range conns {
		conn.Close()
	}

	stats = pool.GetStats()
	if stats.Idle != 3 {
		t.Errorf("expected 3 idle, got %d", stats.Idle)
	}

	pool.Close()
}

func TestDialer(t *testing.T) {
	// Test with a local server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	cfg := DefaultConnPoolConfig()
	dialer := NewDialer("tcp", listener.Addr().String(), cfg)
	pool := dialer.NewPool()

	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	conn.Close()
	pool.Close()
}

func TestPooledConn_Age(t *testing.T) {
	factory := func() (net.Conn, error) {
		return newMockConn(), nil
	}

	cfg := DefaultConnPoolConfig()
	pool := NewConnPool(factory, cfg)

	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	pc, ok := conn.(*pooledConn)
	if !ok {
		t.Fatal("expected pooledConn")
	}

	age := pc.Age()
	if age < 0 {
		t.Errorf("expected non-negative age, got %v", age)
	}
	if age > time.Second {
		t.Errorf("age seems too large: %v", age)
	}

	conn.Close()
	pool.Close()
}

func TestPooledConn_ForceClose(t *testing.T) {
	factory := func() (net.Conn, error) {
		return newMockConn(), nil
	}

	cfg := DefaultConnPoolConfig()
	cfg.MaxIdle = 5
	pool := NewConnPool(factory, cfg)

	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	pc, ok := conn.(*pooledConn)
	if !ok {
		t.Fatal("expected pooledConn")
	}

	// Force close should not return to pool
	err = pc.ForceClose()
	if err != nil {
		t.Fatalf("ForceClose failed: %v", err)
	}

	stats := pool.GetStats()
	if stats.Idle != 0 {
		t.Errorf("expected 0 idle connections after force close, got %d", stats.Idle)
	}

	pool.Close()
}
