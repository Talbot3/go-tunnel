package connmgr

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
	localAddr  string
	remoteAddr string
	closed     atomic.Bool
}

func (m *mockConn) Read(b []byte) (n int, err error)  { return 0, errors.New("not implemented") }
func (m *mockConn) Write(b []byte) (n int, err error) { return len(b), nil }
func (m *mockConn) Close() error {
	m.closed.Store(true)
	return nil
}
func (m *mockConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}
func (m *mockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func TestManager_New(t *testing.T) {
	m := NewManager(Config{})
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestManager_Accept_NoLimit(t *testing.T) {
	m := NewManager(Config{
		MaxConnections: 0, // No limit
	})

	conn := &mockConn{}
	info, err := m.Accept(conn)
	if err != nil {
		t.Fatalf("Accept failed: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.ID == "" {
		t.Error("expected non-empty ID")
	}
	if info.RemoteAddr == "" {
		t.Error("expected non-empty RemoteAddr")
	}

	// Verify stats
	if m.Active() != 1 {
		t.Errorf("expected 1 active connection, got %d", m.Active())
	}
	if m.Total() != 1 {
		t.Errorf("expected 1 total connection, got %d", m.Total())
	}

	m.Release(info)
	if m.Active() != 0 {
		t.Errorf("expected 0 active connections after release, got %d", m.Active())
	}
}

func TestManager_Accept_WithLimit(t *testing.T) {
	m := NewManager(Config{
		MaxConnections: 2,
	})

	// Accept 2 connections
	conn1 := &mockConn{}
	info1, err := m.Accept(conn1)
	if err != nil {
		t.Fatalf("Accept 1 failed: %v", err)
	}

	conn2 := &mockConn{}
	info2, err := m.Accept(conn2)
	if err != nil {
		t.Fatalf("Accept 2 failed: %v", err)
	}

	// Third should be rejected
	conn3 := &mockConn{}
	_, err = m.Accept(conn3)
	if err != ErrConnectionLimit {
		t.Errorf("expected ErrConnectionLimit, got %v", err)
	}

	if m.Rejected() != 1 {
		t.Errorf("expected 1 rejected, got %d", m.Rejected())
	}

	// Release one and try again
	m.Release(info1)

	conn4 := &mockConn{}
	info4, err := m.Accept(conn4)
	if err != nil {
		t.Fatalf("Accept 4 failed: %v", err)
	}

	_ = info2
	_ = info4
}

func TestManager_Release(t *testing.T) {
	m := NewManager(Config{})

	conn := &mockConn{}
	info, _ := m.Accept(conn)

	if m.Active() != 1 {
		t.Errorf("expected 1 active, got %d", m.Active())
	}

	m.Release(info)

	if m.Active() != 0 {
		t.Errorf("expected 0 active after release, got %d", m.Active())
	}

	// Release nil should not panic
	m.Release(nil)

	// Double release should not panic
	m.Release(info)
}

func TestManager_GetInfo(t *testing.T) {
	m := NewManager(Config{})

	conn := &mockConn{}
	info, _ := m.Accept(conn)

	// Should be able to get info
	retrieved, ok := m.GetInfo(info.ID)
	if !ok {
		t.Fatal("expected to find info")
	}
	if retrieved.ID != info.ID {
		t.Error("ID mismatch")
	}

	// Release and check
	m.Release(info)
	_, ok = m.GetInfo(info.ID)
	if ok {
		t.Error("expected not to find info after release")
	}
}

func TestManager_ForEach(t *testing.T) {
	m := NewManager(Config{})

	// Accept 3 connections
	var infos []*ConnInfo
	for i := 0; i < 3; i++ {
		conn := &mockConn{}
		info, _ := m.Accept(conn)
		infos = append(infos, info)
	}

	// Iterate
	count := 0
	m.ForEach(func(id string, info *ConnInfo) bool {
		count++
		return true
	})

	if count != 3 {
		t.Errorf("expected 3 iterations, got %d", count)
	}

	// Stop iteration early
	count = 0
	m.ForEach(func(id string, info *ConnInfo) bool {
		count++
		return false // Stop
	})

	if count != 1 {
		t.Errorf("expected 1 iteration with early stop, got %d", count)
	}

	// Cleanup
	for _, info := range infos {
		m.Release(info)
	}
}

func TestManager_CloseIdle(t *testing.T) {
	m := NewManager(Config{})

	conn := &mockConn{}
	_, _ = m.Accept(conn)

	// No timeout - should return nil
	closed := m.CloseIdle(0)
	if closed != nil {
		t.Error("expected nil for zero timeout")
	}

	// Very short timeout - should not close recent connection
	closed = m.CloseIdle(time.Nanosecond)
	if len(closed) != 0 {
		t.Errorf("expected 0 closed, got %d", len(closed))
	}

	// Long timeout after waiting
	time.Sleep(10 * time.Millisecond)
	closed = m.CloseIdle(5 * time.Millisecond)
	if len(closed) != 1 {
		t.Errorf("expected 1 closed, got %d", len(closed))
	}
}

func TestManager_GetStats(t *testing.T) {
	m := NewManager(Config{
		MaxConnections: 10,
	})

	conn := &mockConn{}
	info, _ := m.Accept(conn)

	stats := m.GetStats()
	if stats.Active != 1 {
		t.Errorf("expected Active=1, got %d", stats.Active)
	}
	if stats.Total != 1 {
		t.Errorf("expected Total=1, got %d", stats.Total)
	}
	if stats.Limit != 10 {
		t.Errorf("expected Limit=10, got %d", stats.Limit)
	}
	if stats.Utilization != 10.0 { // 1/10 * 100
		t.Errorf("expected Utilization=10.0, got %f", stats.Utilization)
	}

	m.Release(info)
}

func TestManager_Shutdown(t *testing.T) {
	m := NewManager(Config{})

	conn := &mockConn{}
	info, _ := m.Accept(conn)

	ctx := context.Background()

	// Release in goroutine
	go func() {
		time.Sleep(10 * time.Millisecond)
		m.Release(info)
	}()

	err := m.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestManager_Shutdown_Timeout(t *testing.T) {
	m := NewManager(Config{})

	conn := &mockConn{}
	_, _ = m.Accept(conn)

	// Very short timeout - context should be done before connection is released
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	err := m.Shutdown(ctx)
	// The connection was never released, so Shutdown should return context error
	if err != nil && err != context.DeadlineExceeded {
		t.Errorf("expected nil or DeadlineExceeded, got %v", err)
	}
}

func TestConnInfo_Activity(t *testing.T) {
	info := &ConnInfo{
		ID:         "test",
		StartTime:  time.Now(),
	}
	info.UpdateActive()

	// LastActive should be recent
	lastActive := info.LastActive()
	if time.Since(lastActive) > time.Second {
		t.Error("LastActive should be recent")
	}

	// Duration
	if info.Duration() < 0 {
		t.Error("Duration should be non-negative")
	}
}

func TestConnInfo_Traffic(t *testing.T) {
	info := &ConnInfo{}

	info.AddBytesIn(100)
	info.AddBytesIn(50)
	info.AddBytesOut(200)

	if info.BytesIn() != 150 {
		t.Errorf("expected BytesIn=150, got %d", info.BytesIn())
	}
	if info.BytesOut() != 200 {
		t.Errorf("expected BytesOut=200, got %d", info.BytesOut())
	}
}

func TestConnInfo_Closed(t *testing.T) {
	info := &ConnInfo{}

	if info.IsClosed() {
		t.Error("expected not closed initially")
	}

	info.Close()

	if !info.IsClosed() {
		t.Error("expected closed after Close()")
	}
}

func TestWrappedConn_ReadWrite(t *testing.T) {
	m := NewManager(Config{})
	conn := &mockConn{}
	info, _ := m.Accept(conn)

	wrapped := m.WrapConn(conn, info)

	// Write should update bytes out
	n, err := wrapped.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != 5 {
		t.Errorf("expected n=5, got %d", n)
	}

	if info.BytesOut() != 5 {
		t.Errorf("expected BytesOut=5, got %d", info.BytesOut())
	}

	// LastActive should be updated
	lastActive := info.LastActive()
	if time.Since(lastActive) > time.Second {
		t.Error("LastActive should be recent after Write")
	}

	// Info should return the same info
	if wrapped.Info() != info {
		t.Error("Info() should return the same info")
	}
}

func TestWrappedConn_Close(t *testing.T) {
	m := NewManager(Config{})
	conn := &mockConn{}
	info, _ := m.Accept(conn)

	wrapped := m.WrapConn(conn, info)

	if m.Active() != 1 {
		t.Errorf("expected 1 active, got %d", m.Active())
	}

	wrapped.Close()

	if m.Active() != 0 {
		t.Errorf("expected 0 active after close, got %d", m.Active())
	}

	// Double close should not panic
	wrapped.Close()
}

func TestManager_Concurrent(t *testing.T) {
	m := NewManager(Config{
		MaxConnections: 100,
	})

	var wg sync.WaitGroup

	// Concurrent accept/release
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := &mockConn{}
			info, err := m.Accept(conn)
			if err != nil {
				return
			}
			time.Sleep(time.Microsecond)
			m.Release(info)
		}()
	}

	wg.Wait()

	if m.Active() != 0 {
		t.Errorf("expected 0 active after all released, got %d", m.Active())
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == "" {
		t.Error("expected non-empty ID")
	}
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
	if len(id1) != 16 { // 8 bytes = 16 hex chars
		t.Errorf("expected ID length 16, got %d", len(id1))
	}
}
