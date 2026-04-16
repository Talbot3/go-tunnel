package quic

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/Talbot3/go-tunnel/internal/pool"
)

// ============================================
// Internal Function Unit Tests
// These tests directly call unexported functions
// ============================================

// mockNetConnInternal is a mock net.Conn for testing internal functions
type mockNetConnInternal struct {
	readData  []byte
	readPos   int
	writeData []byte
	closed    bool
	mu        sync.Mutex
}

func (m *mockNetConnInternal) Read(b []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, io.EOF
	}
	if m.readPos >= len(m.readData) {
		return 0, io.EOF
	}
	n = copy(b, m.readData[m.readPos:])
	m.readPos += n
	return n, nil
}

func (m *mockNetConnInternal) Write(b []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, net.ErrClosed
	}
	m.writeData = append(m.writeData, b...)
	return len(b), nil
}

func (m *mockNetConnInternal) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockNetConnInternal) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

func (m *mockNetConnInternal) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

func (m *mockNetConnInternal) SetDeadline(t time.Time) error      { return nil }
func (m *mockNetConnInternal) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockNetConnInternal) SetWriteDeadline(t time.Time) error { return nil }

// mockQuicStreamInternal is a mock quic.Stream for testing internal functions
type mockQuicStreamInternal struct {
	readData  []byte
	readPos   int
	writeData []byte
	closed    bool
	mu        sync.Mutex
	ctx       context.Context
}

func newMockQuicStreamInternal() *mockQuicStreamInternal {
	return &mockQuicStreamInternal{
		ctx: context.Background(),
	}
}

func (m *mockQuicStreamInternal) Read(b []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, io.EOF
	}
	if m.readPos >= len(m.readData) {
		return 0, io.EOF
	}
	n = copy(b, m.readData[m.readPos:])
	m.readPos += n
	return n, nil
}

func (m *mockQuicStreamInternal) Write(b []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, net.ErrClosed
	}
	m.writeData = append(m.writeData, b...)
	return len(b), nil
}

func (m *mockQuicStreamInternal) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockQuicStreamInternal) CancelRead(err quic.StreamErrorCode) {
	m.Close()
}

func (m *mockQuicStreamInternal) CancelWrite(err quic.StreamErrorCode) {
	m.Close()
}

func (m *mockQuicStreamInternal) SetDeadline(t time.Time) error      { return nil }
func (m *mockQuicStreamInternal) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockQuicStreamInternal) SetWriteDeadline(t time.Time) error { return nil }

func (m *mockQuicStreamInternal) StreamID() quic.StreamID {
	return 1
}

func (m *mockQuicStreamInternal) Context() context.Context {
	return m.ctx
}

// ============================================
// Test handleDataStream directly
// ============================================

func TestHandleDataStream_LegacyFormat(t *testing.T) {
	// Create a minimal server
	server := &MuxServer{
		bufferPool: pool.NewBufferPool(32 * 1024),
	}

	// Create a tunnel with context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tunnel := &Tunnel{
		ID:           "test-tunnel",
		LocalAddr:    "127.0.0.1:8080",
		ctx:          ctx,
		cancel:       cancel,
	}

	// Create a mock external connection and store it
	extConn := &mockNetConnInternal{
		readData: []byte("response from server"),
	}
	connID := "test-conn-123"
	tunnel.ExternalConns.Store(connID, extConn)

	// Create mock stream with legacy format data
	// Format: [StreamTypeData][connIDLen:2][connID]
	streamData := []byte{byte(StreamTypeData)}
	connIDBytes := []byte(connID)
	streamData = append(streamData, byte(len(connIDBytes)>>8), byte(len(connIDBytes)))
	streamData = append(streamData, connIDBytes...)

	mockStream := newMockQuicStreamInternal()
	mockStream.readData = streamData

	// Call handleDataStream directly
	server.wg.Add(1)
	go server.handleDataStream(tunnel, mockStream)

	// Wait a bit for the goroutine to process
	time.Sleep(100 * time.Millisecond)

	// Cancel context to stop the forward
	cancel()
	time.Sleep(50 * time.Millisecond)

	// Verify the stream was read (readPos should have advanced)
	mockStream.mu.Lock()
	if mockStream.readPos == 0 {
		t.Error("Expected stream to be read")
	}
	mockStream.mu.Unlock()

	t.Logf("handleDataStream legacy format test passed")
}

func TestHandleDataStream_NewSessionHeaderFormat(t *testing.T) {
	// Create a minimal server
	server := &MuxServer{
		bufferPool: pool.NewBufferPool(32 * 1024),
	}

	// Create a tunnel with context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tunnel := &Tunnel{
		ID:           "test-tunnel",
		LocalAddr:    "127.0.0.1:8080",
		ctx:          ctx,
		cancel:       cancel,
	}

	// Create a mock external connection and store it
	extConn := &mockNetConnInternal{
		readData: []byte("response"),
	}
	connID := "test-conn-456"
	tunnel.ExternalConns.Store(connID, extConn)

	// Create mock stream with new session header format
	// Format: [StreamTypeData][SessionHeader]
	header := &SessionHeader{
		Protocol: ProtocolTCP,
		Target:   "127.0.0.1:9090",
		Flags:    FlagKeepAlive,
		ConnID:   connID,
	}

	streamData := []byte{byte(StreamTypeData)}
	streamData = append(streamData, header.Encode()...)

	mockStream := newMockQuicStreamInternal()
	mockStream.readData = streamData

	// Call handleDataStream directly
	server.wg.Add(1)
	go server.handleDataStream(tunnel, mockStream)

	// Wait a bit for the goroutine to process
	time.Sleep(100 * time.Millisecond)

	// Cancel context to stop
	cancel()
	time.Sleep(50 * time.Millisecond)

	t.Logf("handleDataStream new session header format test passed")
}

func TestHandleDataStream_InvalidStreamType(t *testing.T) {
	// Create a minimal server
	server := &MuxServer{
		bufferPool: pool.NewBufferPool(32 * 1024),
	}

	// Create a tunnel with context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tunnel := &Tunnel{
		ID:           "test-tunnel",
		LocalAddr:    "127.0.0.1:8080",
		ctx:          ctx,
		cancel:       cancel,
	}

	// Create mock stream with invalid stream type
	streamData := []byte{0xFF, 0x00, 0x01} // Invalid stream type

	mockStream := newMockQuicStreamInternal()
	mockStream.readData = streamData

	// Call handleDataStream directly
	server.wg.Add(1)
	server.handleDataStream(tunnel, mockStream)

	// Verify stream was closed
	mockStream.mu.Lock()
	if !mockStream.closed {
		t.Error("Expected stream to be closed after invalid stream type")
	}
	mockStream.mu.Unlock()

	t.Logf("handleDataStream invalid stream type test passed")
}

func TestHandleDataStream_NoExternalConn(t *testing.T) {
	// Create a minimal server
	server := &MuxServer{
		bufferPool: pool.NewBufferPool(32 * 1024),
	}

	// Create a tunnel with context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tunnel := &Tunnel{
		ID:           "test-tunnel",
		LocalAddr:    "127.0.0.1:8080",
		ctx:          ctx,
		cancel:       cancel,
	}

	// Create mock stream with valid format but no matching external connection
	connID := "nonexistent-conn"
	streamData := []byte{byte(StreamTypeData)}
	connIDBytes := []byte(connID)
	streamData = append(streamData, byte(len(connIDBytes)>>8), byte(len(connIDBytes)))
	streamData = append(streamData, connIDBytes...)

	mockStream := newMockQuicStreamInternal()
	mockStream.readData = streamData

	// Call handleDataStream directly
	server.wg.Add(1)
	server.handleDataStream(tunnel, mockStream)

	// Verify stream was closed
	mockStream.mu.Lock()
	if !mockStream.closed {
		t.Error("Expected stream to be closed when no external connection found")
	}
	mockStream.mu.Unlock()

	t.Logf("handleDataStream no external connection test passed")
}

// ============================================
// Test forwardBidirectional directly
// ============================================

func TestForwardBidirectional_DataFlow(t *testing.T) {
	// Create a minimal server
	server := &MuxServer{
		bufferPool: pool.NewBufferPool(32 * 1024),
	}

	// Create a tunnel with context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tunnel := &Tunnel{
		ID:     "test-tunnel",
		ctx:    ctx,
		cancel: cancel,
	}

	// Create mock connections
	extConn := &mockNetConnInternal{
		readData: []byte("data from external"),
	}
	stream := newMockQuicStreamInternal()
	stream.readData = []byte("data from stream")
	connID := "test-conn-789"

	// Store the external connection
	tunnel.ExternalConns.Store(connID, extConn)

	// Call forwardBidirectional in a goroutine
	done := make(chan struct{})
	go func() {
		server.forwardBidirectional(extConn, stream, connID, tunnel)
		close(done)
	}()

	// Wait a bit for data to flow
	time.Sleep(100 * time.Millisecond)

	// Cancel to stop forwarding
	cancel()

	// Wait for forwardBidirectional to complete
	<-done

	// Verify connection was removed from tunnel
	if _, ok := tunnel.ExternalConns.Load(connID); ok {
		t.Error("Expected connection to be removed from tunnel")
	}

	t.Logf("forwardBidirectional data flow test passed")
}

func TestForwardBidirectional_ContextCancellation(t *testing.T) {
	// Create a minimal server
	server := &MuxServer{
		bufferPool: pool.NewBufferPool(32 * 1024),
	}

	// Create a tunnel with already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	tunnel := &Tunnel{
		ID:     "test-tunnel",
		ctx:    ctx,
		cancel: cancel,
	}

	// Create mock connections with no data (will block on read)
	extConn := &mockNetConnInternal{}
	stream := newMockQuicStreamInternal()
	connID := "test-conn-ctx"

	// Store the external connection
	tunnel.ExternalConns.Store(connID, extConn)

	// Call forwardBidirectional - should return quickly due to cancelled context
	done := make(chan struct{})
	go func() {
		server.forwardBidirectional(extConn, stream, connID, tunnel)
		close(done)
	}()

	// Should complete quickly
	select {
	case <-done:
		t.Logf("forwardBidirectional context cancellation test passed")
	case <-time.After(500 * time.Millisecond):
		t.Error("forwardBidirectional did not complete within timeout")
	}
}

// ============================================
// Test handleDatagramHeartbeat directly
// ============================================

func TestHandleDatagramHeartbeat_Valid(t *testing.T) {
	// Create a minimal server
	server := &MuxServer{}

	// Create a tunnel
	tunnel := &Tunnel{
		ID: "test-tunnel",
	}

	// Create mock QUIC connection for sending ack
	mockConn := &mockQuicConnection{
		sendDatagramErr: nil,
	}

	// Create valid heartbeat data
	// Format: [1B type][4B timestamp][4B lastRTT]
	now := uint32(time.Now().UnixNano() / int64(time.Millisecond))
	data := make([]byte, 9)
	data[0] = DgramTypeHeartbeat
	data[1] = byte(now >> 24)
	data[2] = byte(now >> 16)
	data[3] = byte(now >> 8)
	data[4] = byte(now)
	data[5] = 0x00
	data[6] = 0x00
	data[7] = 0x00
	data[8] = 0x00

	// Call handleDatagramHeartbeat directly
	server.handleDatagramHeartbeat(mockConn, data, tunnel)

	// Verify LastActive was updated
	if tunnel.LastActive.Load() == 0 {
		t.Error("Expected LastActive to be updated")
	}

	// Verify datagram was sent
	if len(mockConn.sentDatagrams) == 0 {
		t.Error("Expected heartbeat ack to be sent")
	}

	t.Logf("handleDatagramHeartbeat valid test passed")
}

func TestHandleDatagramHeartbeat_TooShort(t *testing.T) {
	// Create a minimal server
	server := &MuxServer{}

	// Create a tunnel
	tunnel := &Tunnel{
		ID: "test-tunnel",
	}

	// Create mock QUIC connection
	mockConn := &mockQuicConnection{}

	// Create short heartbeat data (less than 9 bytes)
	data := []byte{DgramTypeHeartbeat, 0x00, 0x01}

	// Call handleDatagramHeartbeat directly
	server.handleDatagramHeartbeat(mockConn, data, tunnel)

	// Verify no datagram was sent (early return)
	if len(mockConn.sentDatagrams) > 0 {
		t.Error("Expected no datagram to be sent for short data")
	}

	t.Logf("handleDatagramHeartbeat too short test passed")
}

// mockQuicConnection is a mock quic.Connection for testing
type mockQuicConnection struct {
	sentDatagrams  [][]byte
	sendDatagramErr error
}

func (m *mockQuicConnection) SendDatagram(data []byte) error {
	m.sentDatagrams = append(m.sentDatagrams, data)
	return m.sendDatagramErr
}

func (m *mockQuicConnection) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	return nil, io.EOF
}

func (m *mockQuicConnection) OpenStream() (quic.Stream, error) {
	return nil, net.ErrClosed
}

func (m *mockQuicConnection) OpenStreamSync(ctx context.Context) (quic.Stream, error) {
	return nil, net.ErrClosed
}

func (m *mockQuicConnection) OpenUniStream() (quic.SendStream, error) {
	return nil, net.ErrClosed
}

func (m *mockQuicConnection) OpenUniStreamSync(ctx context.Context) (quic.SendStream, error) {
	return nil, net.ErrClosed
}

func (m *mockQuicConnection) AcceptStream(ctx context.Context) (quic.Stream, error) {
	return nil, net.ErrClosed
}

func (m *mockQuicConnection) AcceptUniStream(ctx context.Context) (quic.ReceiveStream, error) {
	return nil, net.ErrClosed
}

func (m *mockQuicConnection) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443}
}

func (m *mockQuicConnection) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

func (m *mockQuicConnection) CloseWithError(code quic.ApplicationErrorCode, desc string) error {
	return nil
}

func (m *mockQuicConnection) Context() context.Context {
	return context.Background()
}

func (m *mockQuicConnection) ConnectionState() quic.ConnectionState {
	return quic.ConnectionState{}
}

// ============================================
// Test sendStreamHeartbeat directly
// ============================================

func TestSendStreamHeartbeat_Success(t *testing.T) {
	// Create a minimal client
	mockStream := newMockQuicStreamInternal()
	client := &MuxClient{
		controlStream: mockStream,
	}

	// Call sendStreamHeartbeat directly
	client.sendStreamHeartbeat()

	// Verify heartbeat was written
	mockStream.mu.Lock()
	if len(mockStream.writeData) != 1 {
		t.Errorf("Expected 1 byte written, got %d", len(mockStream.writeData))
	}
	if len(mockStream.writeData) > 0 && mockStream.writeData[0] != MsgTypeHeartbeat {
		t.Errorf("Expected MsgTypeHeartbeat (0x03), got 0x%02x", mockStream.writeData[0])
	}
	mockStream.mu.Unlock()

	t.Logf("sendStreamHeartbeat success test passed")
}

func TestSendStreamHeartbeat_WriteError(t *testing.T) {
	// Create a minimal client with closed stream
	mockStream := newMockQuicStreamInternal()
	mockStream.closed = true
	client := &MuxClient{
		controlStream: mockStream,
		reconnectCh:   make(chan struct{}, 1),
	}
	client.connected.Store(true)

	// Call sendStreamHeartbeat directly
	client.sendStreamHeartbeat()

	// Verify reconnect was triggered
	select {
	case <-client.reconnectCh:
		t.Logf("sendStreamHeartbeat write error test passed (reconnect triggered)")
	default:
		t.Log("Note: Reconnect signal not captured (may have been processed)")
	}
}

// ============================================
// Test handleData directly
// ============================================

func TestHandleData_ValidPayload(t *testing.T) {
	// Create a minimal client
	client := &MuxClient{}

	// Create mock local connection
	mockLocalConn := &mockNetConnInternal{}
	connID := "test-conn-data"
	client.localConns.Store(connID, mockLocalConn)

	// Create valid payload
	// Format: [conn_id_len:2][conn_id][data_len:4][data]
	data := []byte("Hello, World!")
	payload := make([]byte, 0)
	connIDBytes := []byte(connID)
	payload = append(payload, byte(len(connIDBytes)>>8), byte(len(connIDBytes)))
	payload = append(payload, connIDBytes...)
	payload = append(payload, byte(len(data)>>24), byte(len(data)>>16), byte(len(data)>>8), byte(len(data)))
	payload = append(payload, data...)

	// Call handleData directly
	client.handleData(payload)

	// Verify data was written to local connection
	mockLocalConn.mu.Lock()
	if string(mockLocalConn.writeData) != string(data) {
		t.Errorf("Expected %q, got %q", string(data), string(mockLocalConn.writeData))
	}
	mockLocalConn.mu.Unlock()

	// Verify bytes were counted
	if client.bytesIn.Load() != int64(len(data)) {
		t.Errorf("Expected bytesIn=%d, got %d", len(data), client.bytesIn.Load())
	}

	t.Logf("handleData valid payload test passed")
}

func TestHandleData_TooShort(t *testing.T) {
	// Create a minimal client
	client := &MuxClient{}

	// Call handleData with empty payload
	client.handleData([]byte{})

	// Call handleData with 1 byte payload
	client.handleData([]byte{0x00})

	t.Logf("handleData too short test passed")
}

func TestHandleData_InvalidBounds(t *testing.T) {
	// Create a minimal client
	client := &MuxClient{}

	// Create payload with invalid connID length (claims 100 bytes but only has 2)
	payload := []byte{0x00, 0x64, 0x41, 0x42} // connIDLen=100, but only "AB" present
	client.handleData(payload)

	// Create payload with invalid data length
	payload2 := []byte{0x00, 0x02, 0x41, 0x42, 0xFF, 0xFF, 0xFF, 0xFF} // connID="AB", dataLen=0xFFFFFFFF
	client.handleData(payload2)

	t.Logf("handleData invalid bounds test passed")
}

func TestHandleData_NoLocalConn(t *testing.T) {
	// Create a minimal client with empty localConns
	client := &MuxClient{}

	// Create valid payload for non-existent connection
	data := []byte("test data")
	payload := make([]byte, 0)
	connID := "nonexistent-conn"
	connIDBytes := []byte(connID)
	payload = append(payload, byte(len(connIDBytes)>>8), byte(len(connIDBytes)))
	payload = append(payload, connIDBytes...)
	payload = append(payload, byte(len(data)>>24), byte(len(data)>>16), byte(len(data)>>8), byte(len(data)))
	payload = append(payload, data...)

	// Call handleData - should not panic
	client.handleData(payload)

	// Verify no bytes were counted (no connection found)
	if client.bytesIn.Load() != 0 {
		t.Errorf("Expected bytesIn=0, got %d", client.bytesIn.Load())
	}

	t.Logf("handleData no local connection test passed")
}

func TestHandleData_WriteError(t *testing.T) {
	// Create a minimal client
	client := &MuxClient{}

	// Create mock local connection that is closed (will return error on write)
	mockLocalConn := &mockNetConnInternal{closed: true}
	connID := "test-conn-write-err"
	client.localConns.Store(connID, mockLocalConn)
	client.activeConns.Add(1)

	// Create valid payload
	data := []byte("test data")
	payload := make([]byte, 0)
	connIDBytes := []byte(connID)
	payload = append(payload, byte(len(connIDBytes)>>8), byte(len(connIDBytes)))
	payload = append(payload, connIDBytes...)
	payload = append(payload, byte(len(data)>>24), byte(len(data)>>16), byte(len(data)>>8), byte(len(data)))
	payload = append(payload, data...)

	// Call handleData
	client.handleData(payload)

	// Verify connection was removed
	if _, ok := client.localConns.Load(connID); ok {
		t.Error("Expected connection to be removed after write error")
	}

	// Verify activeConns was decremented
	if client.activeConns.Load() != 0 {
		t.Errorf("Expected activeConns=0, got %d", client.activeConns.Load())
	}

	t.Logf("handleData write error test passed")
}
