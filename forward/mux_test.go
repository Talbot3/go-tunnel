package forward

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// ============================================
// Encoder/Decoder Tests
// ============================================

func TestDefaultMuxEncoderDecoder(t *testing.T) {
	encoder := NewDefaultMuxEncoder()
	decoder := NewDefaultMuxDecoder()

	tests := []struct {
		name     string
		encode   func() ([]byte, error)
		expected MessageType
		id       string
		payload  []byte
	}{
		{
			name: "DATA message",
			encode: func() ([]byte, error) {
				return encoder.EncodeData("conn123", []byte("hello world"))
			},
			expected: MsgTypeData,
			id:       "conn123",
			payload:  []byte("hello world"),
		},
		{
			name: "CLOSE message",
			encode: func() ([]byte, error) {
				return encoder.EncodeClose("conn123")
			},
			expected: MsgTypeClose,
			id:       "conn123",
			payload:  nil,
		},
		{
			name: "REQUEST message",
			encode: func() ([]byte, error) {
				return encoder.EncodeRequest("req456", []byte("GET / HTTP/1.1\r\n\r\n"))
			},
			expected: MsgTypeRequest,
			id:       "req456",
			payload:  []byte("GET / HTTP/1.1\r\n\r\n"),
		},
		{
			name: "RESPONSE message",
			encode: func() ([]byte, error) {
				return encoder.EncodeResponse("req456", []byte("HTTP/1.1 200 OK\r\n\r\n"))
			},
			expected: MsgTypeResponse,
			id:       "req456",
			payload:  []byte("HTTP/1.1 200 OK\r\n\r\n"),
		},
		{
			name: "NEWCONN message",
			encode: func() ([]byte, error) {
				return encoder.EncodeNewConn("conn789", "192.168.1.1:12345")
			},
			expected: MsgTypeNewConn,
			id:       "conn789",
			payload:  []byte("192.168.1.1:12345"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := tt.encode()
			if err != nil {
				t.Fatalf("encode failed: %v", err)
			}

			msgType, id, payload, err := decoder.Decode(encoded)
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}

			if msgType != tt.expected {
				t.Errorf("expected type %v, got %v", tt.expected, msgType)
			}

			if id != tt.id {
				t.Errorf("expected id %s, got %s", tt.id, id)
			}

			if !bytes.Equal(payload, tt.payload) {
				t.Errorf("expected payload %q, got %q", tt.payload, payload)
			}
		})
	}
}

func TestDefaultMuxEncoder_LargePayload(t *testing.T) {
	encoder := NewDefaultMuxEncoder()
	decoder := NewDefaultMuxDecoder()

	// Create a large payload
	largePayload := make([]byte, 64*1024)
	for i := range largePayload {
		largePayload[i] = byte(i % 256)
	}

	encoded, err := encoder.EncodeData("conn1", largePayload)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	msgType, id, payload, err := decoder.Decode(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if msgType != MsgTypeData {
		t.Errorf("expected DATA, got %v", msgType)
	}

	if id != "conn1" {
		t.Errorf("expected id conn1, got %s", id)
	}

	if !bytes.Equal(payload, largePayload) {
		t.Errorf("payload mismatch")
	}
}

// ============================================
// MuxForwarder Tests
// ============================================

func TestMuxForwarder(t *testing.T) {
	encoder := NewDefaultMuxEncoder()
	decoder := NewDefaultMuxDecoder()

	// Create pipe connections
	localReader, localWriter := io.Pipe()
	muxReader, muxWriter := io.Pipe()

	localConn := &pipeConn{reader: localReader, writer: localWriter}
	muxConn := &pipeConn{reader: muxReader, writer: muxWriter}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	forwarder := NewMuxForwarder()

	// Start forwarder
	done := make(chan error, 1)
	go func() {
		done <- forwarder.ForwardMux(ctx, localConn, muxConn, "conn1", encoder)
	}()

	// Write data to local connection
	go func() {
		localConn.Write([]byte("hello"))
		time.Sleep(100 * time.Millisecond)
		localConn.Write([]byte("world"))
		time.Sleep(100 * time.Millisecond)
		localConn.Close()
	}()

	// Read from mux connection
	buf := make([]byte, 1024)
	n, err := muxConn.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	msgType, id, payload, err := decoder.Decode(buf[:n])
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if msgType != MsgTypeData {
		t.Errorf("expected DATA, got %v", msgType)
	}

	if string(payload) != "hello" {
		t.Errorf("expected 'hello', got %q", string(payload))
	}

	t.Logf("Received message: type=%v, id=%s, payload=%q", msgType, id, payload)
}

// ============================================
// BidirectionalMuxForwarder Tests
// ============================================

func TestBidirectionalMuxForwarder(t *testing.T) {
	encoder := NewDefaultMuxEncoder()
	decoder := NewDefaultMuxDecoder()

	// Create local connection pair
	local1, local2 := net.Pipe()

	// Create mux connection pair
	mux1, mux2 := net.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	forwarder := NewBidirectionalMuxForwarder()

	// Start forwarder on one side
	done := make(chan error, 1)
	go func() {
		done <- forwarder.ForwardBidirectionalMux(ctx, local1, mux1, "conn1", encoder, decoder)
	}()

	// Simulate remote side
	var wg sync.WaitGroup
	wg.Add(2)

	// Remote -> Local
	go func() {
		defer wg.Done()
		data, _ := encoder.EncodeData("conn1", []byte("from remote"))
		mux2.Write(data)
		time.Sleep(100 * time.Millisecond)
	}()

	// Local -> Remote
	go func() {
		defer wg.Done()
		local2.Write([]byte("from local"))
		time.Sleep(100 * time.Millisecond)
	}()

	// Read from local2 (received from remote)
	buf := make([]byte, 1024)
	n, err := local2.Read(buf)
	if err != nil {
		t.Fatalf("read local2 failed: %v", err)
	}
	if string(buf[:n]) != "from remote" {
		t.Errorf("expected 'from remote', got %q", string(buf[:n]))
	}

	// Read from mux2 (received from local)
	n, err = mux2.Read(buf)
	if err != nil {
		t.Fatalf("read mux2 failed: %v", err)
	}
	msgType, _, payload, err := decoder.Decode(buf[:n])
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if msgType != MsgTypeData || string(payload) != "from local" {
		t.Errorf("unexpected message: type=%v, payload=%q", msgType, string(payload))
	}

	local2.Close()
	mux2.Close()
	wg.Wait()
}

// ============================================
// MuxConnManager Tests
// ============================================

func TestMuxConnManager_AddRemove(t *testing.T) {
	encoder := NewDefaultMuxEncoder()
	decoder := NewDefaultMuxDecoder()

	// Create mock mux connection
	muxReader, muxWriter := io.Pipe()

	// Create manager
	mgr := NewMuxConnManager(struct {
		io.Reader
		io.Writer
	}{muxReader, muxWriter}, encoder, decoder)
	defer mgr.Close()

	// Create local connection
	local1, local2 := net.Pipe()

	// Add connection
	mgr.AddConnection("conn1", local1)

	// Check stats
	stats := mgr.Stats()
	if stats.ActiveConnections != 1 {
		t.Errorf("expected 1 active connection, got %d", stats.ActiveConnections)
	}
	if stats.TotalConnections != 1 {
		t.Errorf("expected 1 total connection, got %d", stats.TotalConnections)
	}

	// Get connection
	conn, ok := mgr.GetConnection("conn1")
	if !ok || conn != local1 {
		t.Error("failed to get connection")
	}

	// Remove connection
	mgr.RemoveConnection("conn1")

	stats = mgr.Stats()
	if stats.ActiveConnections != 0 {
		t.Errorf("expected 0 active connections, got %d", stats.ActiveConnections)
	}

	// Cleanup
	local2.Close()
	muxReader.Close()
	muxWriter.Close()
}

func TestMuxConnManager_HandleIncoming(t *testing.T) {
	encoder := NewDefaultMuxEncoder()
	decoder := NewDefaultMuxDecoder()

	// Create mock mux connection
	muxReader, muxWriter := io.Pipe()

	// Create manager
	mgr := NewMuxConnManager(struct {
		io.Reader
		io.Writer
	}{muxReader, muxWriter}, encoder, decoder)
	defer mgr.Close()

	// Create local connection pair
	local1, local2 := net.Pipe()
	defer local2.Close()

	// Add connection (but don't start forwarder - we'll handle manually)
	mgr.connections.Store("conn1", local1)
	mgr.activeConns.Add(1)

	// Test HandleIncoming with DATA message
	dataMsg, _ := encoder.EncodeData("conn1", []byte("hello"))
	done := make(chan struct{})
	go func() {
		mgr.HandleIncoming(dataMsg)
		close(done)
	}()

	// Read from local2
	buf := make([]byte, 1024)
	n, err := local2.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	<-done

	if string(buf[:n]) != "hello" {
		t.Errorf("expected 'hello', got %q", string(buf[:n]))
	}

	// Test HandleIncoming with CLOSE message
	closeMsg, _ := encoder.EncodeClose("conn1")
	mgr.HandleIncoming(closeMsg)

	// Connection should be removed
	_, ok := mgr.GetConnection("conn1")
	if ok {
		t.Error("connection should be removed after CLOSE")
	}

	// Cleanup
	local1.Close()
	muxReader.Close()
	muxWriter.Close()
}

// ============================================
// HTTPMuxForwarder Tests
// ============================================

func TestHTTPMuxForwarder(t *testing.T) {
	encoder := NewDefaultMuxEncoder()
	decoder := NewDefaultMuxDecoder()
	forwarder := NewHTTPMuxForwarder()

	// Create mux connection pair
	mux1, mux2 := net.Pipe()

	// Simulate server responding
	go func() {
		buf := make([]byte, 1024)
		n, _ := mux2.Read(buf)

		// Decode request
		msgType, id, _, err := decoder.Decode(buf[:n])
		if err != nil || msgType != MsgTypeRequest {
			return
		}

		// Send response
		respData := []byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nHello")
		resp, _ := encoder.EncodeResponse(id, respData)
		mux2.Write(resp)
	}()

	ctx := context.Background()
	reqData := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")

	resp, err := forwarder.ForwardHTTP(ctx, reqData, mux1, "req1", encoder, decoder, 2*time.Second)
	if err != nil {
		t.Fatalf("ForwardHTTP failed: %v", err)
	}

	if !bytes.Contains(resp, []byte("200 OK")) {
		t.Errorf("expected 200 OK in response, got %q", string(resp))
	}

	t.Logf("Response: %q", string(resp))
}

// ============================================
// Helper Types
// ============================================

// pipeConn wraps io.Pipe into a net.Conn
type pipeConn struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (c *pipeConn) Read(b []byte) (n int, err error)  { return c.reader.Read(b) }
func (c *pipeConn) Write(b []byte) (n int, err error) { return c.writer.Write(b) }
func (c *pipeConn) Close() error {
	c.reader.Close()
	c.writer.Close()
	return nil
}
func (c *pipeConn) LocalAddr() net.Addr                { return nil }
func (c *pipeConn) RemoteAddr() net.Addr               { return nil }
func (c *pipeConn) SetDeadline(t time.Time) error      { return nil }
func (c *pipeConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *pipeConn) SetWriteDeadline(t time.Time) error { return nil }

// ============================================
// Benchmarks
// ============================================

func BenchmarkMuxEncoder_EncodeData(b *testing.B) {
	encoder := NewDefaultMuxEncoder()
	data := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoder.EncodeData("conn1", data)
	}
}

func BenchmarkMuxDecoder_Decode(b *testing.B) {
	encoder := NewDefaultMuxEncoder()
	decoder := NewDefaultMuxDecoder()
	data := make([]byte, 1024)
	encoded, _ := encoder.EncodeData("conn1", data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoder.Decode(encoded)
	}
}

func BenchmarkMuxForwarder_ForwardMux(b *testing.B) {
	encoder := NewDefaultMuxEncoder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create connections
		local1, local2 := net.Pipe()
		mux1, mux2 := net.Pipe()

		ctx, cancel := context.WithCancel(context.Background())
		forwarder := NewMuxForwarder()

		go forwarder.ForwardMux(ctx, local1, mux1, "conn1", encoder)

		// Write and read
		go func() {
			local2.Write([]byte("test"))
			local2.Close()
		}()

		buf := make([]byte, 1024)
		mux2.Read(buf)

		cancel()
		local2.Close()
		mux2.Close()
	}
}

// ============================================
// Binary Protocol Benchmarks (Optimized)
// ============================================

func BenchmarkBinaryProtocol_Encode(b *testing.B) {
	p := NewBinaryProtocol()
	data := make([]byte, 1024)
	msg := &Message{Type: MsgTypeData, ID: "conn1", Payload: data}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, _ := p.Encode(msg)
		p.Release(encoded)
	}
}

func BenchmarkBinaryProtocol_Decode(b *testing.B) {
	p := NewBinaryProtocol()
	data := make([]byte, 1024)
	msg := &Message{Type: MsgTypeData, ID: "conn1", Payload: data}
	encoded, _ := p.Encode(msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Decode(encoded)
	}
}

func BenchmarkBinaryProtocol_EncodeDecode(b *testing.B) {
	p := NewBinaryProtocol()
	data := make([]byte, 1024)
	msg := &Message{Type: MsgTypeData, ID: "conn1", Payload: data}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, _ := p.Encode(msg)
		p.Decode(encoded)
		p.Release(encoded)
	}
}

// Zero-allocation encoder benchmark
func BenchmarkBinaryProtocol_Encode_ZeroAlloc(b *testing.B) {
	p := NewBinaryProtocol()
	data := make([]byte, 1024)
	msg := &Message{Type: MsgTypeData, ID: "conn1", Payload: data}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, _ := p.Encode(msg)
		p.Release(encoded)
	}
}

// ============================================
// Stress Tests for DefaultMuxEncoder
// ============================================

func TestDefaultMuxEncoder_ConcurrentStress(t *testing.T) {
	encoder := NewDefaultMuxEncoder()
	decoder := NewDefaultMuxDecoder()

	const (
		goroutines = 100
		iterations = 10000
		dataSize   = 1024
	)

	data := make([]byte, dataSize)
	for i := range data {
		data[i] = byte(i % 256)
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)

	start := time.Now()

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			connID := fmt.Sprintf("conn%d", id)

			for i := 0; i < iterations; i++ {
				// Encode
				encoded, err := encoder.EncodeData(connID, data)
				if err != nil {
					t.Errorf("encode failed: %v", err)
					return
				}

				// Decode
				msgType, id2, payload, err := decoder.Decode(encoded)
				if err != nil {
					t.Errorf("decode failed: %v", err)
					return
				}

				if msgType != MsgTypeData {
					t.Errorf("expected DATA, got %v", msgType)
					return
				}

				if id2 != connID {
					t.Errorf("expected connID %s, got %s", connID, id2)
					return
				}

				if len(payload) != dataSize {
					t.Errorf("expected payload size %d, got %d", dataSize, len(payload))
					return
				}
			}
		}(g)
	}

	wg.Wait()

	elapsed := time.Since(start)
	totalOps := goroutines * iterations * 2 // encode + decode
	opsPerSec := float64(totalOps) / elapsed.Seconds()

	t.Logf("Concurrent stress test: %d goroutines x %d iterations", goroutines, iterations)
	t.Logf("Total operations: %d (encode + decode)", totalOps)
	t.Logf("Duration: %v", elapsed)
	t.Logf("Throughput: %.2f ops/sec", opsPerSec)
	t.Logf("Latency: %v per op", elapsed/time.Duration(totalOps))
}

func TestDefaultMuxEncoder_Throughput(t *testing.T) {
	encoder := NewDefaultMuxEncoder()

	const (
		totalMessages = 1000000
		dataSize      = 1024
	)

	data := make([]byte, dataSize)
	connID := "conn1"

	start := time.Now()

	for i := 0; i < totalMessages; i++ {
		encoded, _ := encoder.EncodeData(connID, data)
		encoder.Release(encoded)
	}

	elapsed := time.Since(start)
	msgsPerSec := float64(totalMessages) / elapsed.Seconds()
	mbPerSec := msgsPerSec * float64(dataSize) / 1024 / 1024

	t.Logf("Throughput test: %d messages", totalMessages)
	t.Logf("Duration: %v", elapsed)
	t.Logf("Messages/sec: %.2f", msgsPerSec)
	t.Logf("Throughput: %.2f MB/sec", mbPerSec)
}

func TestDefaultMuxEncoder_MixedOperations(t *testing.T) {
	encoder := NewDefaultMuxEncoder()
	decoder := NewDefaultMuxDecoder()

	const iterations = 100000

	data := make([]byte, 512)
	start := time.Now()

	for i := 0; i < iterations; i++ {
		connID := fmt.Sprintf("conn%d", i%100)

		switch i % 5 {
		case 0:
			// DATA
			encoded, _ := encoder.EncodeData(connID, data)
			decoder.Decode(encoded)
		case 1:
			// CLOSE
			encoded, _ := encoder.EncodeClose(connID)
			decoder.Decode(encoded)
		case 2:
			// REQUEST
			encoded, _ := encoder.EncodeRequest(connID, data)
			decoder.Decode(encoded)
		case 3:
			// RESPONSE
			encoded, _ := encoder.EncodeResponse(connID, data)
			decoder.Decode(encoded)
		case 4:
			// NEWCONN
			encoded, _ := encoder.EncodeNewConn(connID, "192.168.1.1:12345")
			decoder.Decode(encoded)
		}
	}

	elapsed := time.Since(start)
	opsPerSec := float64(iterations*2) / elapsed.Seconds()

	t.Logf("Mixed operations test: %d iterations", iterations)
	t.Logf("Duration: %v", elapsed)
	t.Logf("Ops/sec: %.2f", opsPerSec)
}

func TestDefaultMuxEncoder_VaryingPayloadSizes(t *testing.T) {
	encoder := NewDefaultMuxEncoder()
	decoder := NewDefaultMuxDecoder()

	sizes := []int{64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536}
	const iterationsPerSize = 10000

	for _, size := range sizes {
		data := make([]byte, size)
		start := time.Now()

		for i := 0; i < iterationsPerSize; i++ {
			encoded, _ := encoder.EncodeData("conn1", data)
			decoder.Decode(encoded)
		}

		elapsed := time.Since(start)
		opsPerSec := float64(iterationsPerSize*2) / elapsed.Seconds()
		mbPerSec := opsPerSec * float64(size) / 1024 / 1024 / 2

		t.Logf("Size %6d bytes: %8.2f ops/sec, %6.2f MB/sec", size, opsPerSec, mbPerSec)
	}
}

// Benchmark with different payload sizes
func BenchmarkDefaultMuxEncoder_SmallPayload(b *testing.B) {
	encoder := NewDefaultMuxEncoder()
	data := make([]byte, 64)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoder.EncodeData("conn1", data)
	}
}

func BenchmarkDefaultMuxEncoder_MediumPayload(b *testing.B) {
	encoder := NewDefaultMuxEncoder()
	data := make([]byte, 4096)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoder.EncodeData("conn1", data)
	}
}

func BenchmarkDefaultMuxEncoder_LargePayload(b *testing.B) {
	encoder := NewDefaultMuxEncoder()
	data := make([]byte, 65536)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoder.EncodeData("conn1", data)
	}
}

func BenchmarkDefaultMuxEncoder_Parallel(b *testing.B) {
	encoder := NewDefaultMuxEncoder()
	data := make([]byte, 1024)

	b.RunParallel(func(pb *testing.PB) {
		connID := "conn1"
		for pb.Next() {
			encoder.EncodeData(connID, data)
		}
	})
}

func BenchmarkDefaultMuxEncoder_EncodeDecode_Parallel(b *testing.B) {
	encoder := NewDefaultMuxEncoder()
	decoder := NewDefaultMuxDecoder()
	data := make([]byte, 1024)

	b.RunParallel(func(pb *testing.PB) {
		connID := "conn1"
		for pb.Next() {
			encoded, _ := encoder.EncodeData(connID, data)
			decoder.Decode(encoded)
		}
	})
}
