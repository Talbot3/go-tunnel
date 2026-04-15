// Package forward provides platform-optimized data forwarding with multiplexing support.
//
// This file extends the forward package to support multiplexed connections,
// where multiple virtual connections share a single physical connection (e.g., QUIC stream).
//
// # Architecture
//
// Original Forwarder:
//
//	Forward(src, dst) - Direct bidirectional forwarding, no protocol encapsulation
//
// Extended MuxForwarder:
//
//	ForwardMux(localConn, muxConn, connID, encoder) - Forward with protocol encapsulation
//
// # Use Cases
//
//   - Server TCP Handler: Use Forwarder (independent connections)
//   - Client TCP Tunnel: Use MuxForwarder (shared stream with protocol prefix)
//   - HTTP Request-Response: Use HTTPMuxForwarder (non-bidirectional)
package forward

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================
// Message Types
// ============================================

// MessageType represents the type of multiplexed message.
type MessageType int

const (
	// MsgTypeData is a data message for TCP forwarding.
	MsgTypeData MessageType = iota
	// MsgTypeClose is a connection close notification.
	MsgTypeClose
	// MsgTypeRequest is an HTTP request message.
	MsgTypeRequest
	// MsgTypeResponse is an HTTP response message.
	MsgTypeResponse
	// MsgTypeHandshake is a handshake message.
	MsgTypeHandshake
	// MsgTypeNewConn is a new connection notification (server -> client).
	MsgTypeNewConn
)

// String returns the string representation of the message type.
func (mt MessageType) String() string {
	switch mt {
	case MsgTypeData:
		return "DATA"
	case MsgTypeClose:
		return "CLOSE"
	case MsgTypeRequest:
		return "REQUEST"
	case MsgTypeResponse:
		return "RESPONSE"
	case MsgTypeHandshake:
		return "HANDSHAKE"
	case MsgTypeNewConn:
		return "NEWCONN"
	default:
		return "UNKNOWN"
	}
}

// ============================================
// Encoder Interface
// ============================================

// MuxEncoder defines the interface for encoding multiplexed messages.
// Implementations can use different protocols (text, binary, etc.).
type MuxEncoder interface {
	// EncodeData encodes a data message.
	// Format: DATA:<conn_id>:<length>:<payload>
	EncodeData(connID string, data []byte) ([]byte, error)

	// EncodeClose encodes a close message.
	// Format: CLOSE:<conn_id>
	EncodeClose(connID string) ([]byte, error)

	// EncodeRequest encodes an HTTP request message.
	// Format: REQUEST:<req_id>:<length>:<payload>
	EncodeRequest(reqID string, data []byte) ([]byte, error)

	// EncodeResponse encodes an HTTP response message.
	// Format: RESPONSE:<req_id>:<length>:<payload>
	EncodeResponse(reqID string, data []byte) ([]byte, error)

	// EncodeNewConn encodes a new connection notification.
	// Format: NEWCONN:<conn_id>:<remote_addr>
	EncodeNewConn(connID, remoteAddr string) ([]byte, error)
}

// ============================================
// Decoder Interface
// ============================================

// MuxDecoder defines the interface for decoding multiplexed messages.
type MuxDecoder interface {
	// Decode decodes a message.
	// Returns the message type, ID (conn_id or req_id), payload, and error.
	Decode(data []byte) (msgType MessageType, id string, payload []byte, err error)
}

// ============================================
// Default Encoder Implementation
// ============================================

// DefaultMuxEncoder implements MuxEncoder with a text-based protocol.
//
// Message format:
//
//	DATA:<conn_id>:<length>:<payload>\n
//	CLOSE:<conn_id>\n
//	REQUEST:<req_id>:<length>:<payload>\n
//	RESPONSE:<req_id>:<length>:<payload>\n
//	NEWCONN:<conn_id>:<remote_addr>\n
type DefaultMuxEncoder struct {
	// Delimiter is the message delimiter, default is '\n'.
	Delimiter byte
}

// NewDefaultMuxEncoder creates a new default mux encoder.
func NewDefaultMuxEncoder() *DefaultMuxEncoder {
	return &DefaultMuxEncoder{Delimiter: '\n'}
}

// EncodeData encodes a data message.
func (e *DefaultMuxEncoder) EncodeData(connID string, data []byte) ([]byte, error) {
	header := fmt.Sprintf("DATA:%s:%d:", connID, len(data))
	result := make([]byte, len(header)+len(data)+1)
	copy(result, header)
	copy(result[len(header):], data)
	result[len(result)-1] = e.Delimiter
	return result, nil
}

// EncodeClose encodes a close message.
func (e *DefaultMuxEncoder) EncodeClose(connID string) ([]byte, error) {
	return []byte(fmt.Sprintf("CLOSE:%s%c", connID, e.Delimiter)), nil
}

// EncodeRequest encodes an HTTP request message.
func (e *DefaultMuxEncoder) EncodeRequest(reqID string, data []byte) ([]byte, error) {
	header := fmt.Sprintf("REQUEST:%s:%d:", reqID, len(data))
	result := make([]byte, len(header)+len(data)+1)
	copy(result, header)
	copy(result[len(header):], data)
	result[len(result)-1] = e.Delimiter
	return result, nil
}

// EncodeResponse encodes an HTTP response message.
func (e *DefaultMuxEncoder) EncodeResponse(reqID string, data []byte) ([]byte, error) {
	header := fmt.Sprintf("RESPONSE:%s:%d:", reqID, len(data))
	result := make([]byte, len(header)+len(data)+1)
	copy(result, header)
	copy(result[len(header):], data)
	result[len(result)-1] = e.Delimiter
	return result, nil
}

// EncodeNewConn encodes a new connection notification.
func (e *DefaultMuxEncoder) EncodeNewConn(connID, remoteAddr string) ([]byte, error) {
	return []byte(fmt.Sprintf("NEWCONN:%s:%s%c", connID, remoteAddr, e.Delimiter)), nil
}

// ============================================
// Default Decoder Implementation
// ============================================

// DefaultMuxDecoder implements MuxDecoder for the text-based protocol.
type DefaultMuxDecoder struct {
	Delimiter byte
}

// NewDefaultMuxDecoder creates a new default mux decoder.
func NewDefaultMuxDecoder() *DefaultMuxDecoder {
	return &DefaultMuxDecoder{Delimiter: '\n'}
}

// Decode decodes a message.
func (d *DefaultMuxDecoder) Decode(data []byte) (MessageType, string, []byte, error) {
	// Remove trailing delimiter
	if len(data) > 0 && data[len(data)-1] == d.Delimiter {
		data = data[:len(data)-1]
	}

	// Parse format: TYPE:ID:... or TYPE:ID:LENGTH:PAYLOAD
	parts := bytes.SplitN(data, []byte(":"), 4)
	if len(parts) < 2 {
		return MsgTypeData, "", nil, fmt.Errorf("invalid message format: %s", string(data))
	}

	msgTypeStr := string(parts[0])
	id := string(parts[1])

	var msgType MessageType
	switch msgTypeStr {
	case "DATA":
		msgType = MsgTypeData
	case "CLOSE":
		msgType = MsgTypeClose
	case "REQUEST":
		msgType = MsgTypeRequest
	case "RESPONSE":
		msgType = MsgTypeResponse
	case "HANDSHAKE":
		msgType = MsgTypeHandshake
	case "NEWCONN":
		msgType = MsgTypeNewConn
	default:
		return MsgTypeData, "", nil, fmt.Errorf("unknown message type: %s", msgTypeStr)
	}

	// For DATA/REQUEST/RESPONSE, parse length and payload
	var payload []byte
	if len(parts) >= 4 {
		switch msgType {
		case MsgTypeData, MsgTypeRequest, MsgTypeResponse:
			length, err := strconv.Atoi(string(parts[2]))
			if err != nil {
				return msgType, id, nil, fmt.Errorf("invalid length: %s", string(parts[2]))
			}
			payload = parts[3]
			if len(payload) > length {
				payload = payload[:length]
			}
		}
	}

	// For NEWCONN, payload is remote_addr (may contain colons like IP:port)
	if msgType == MsgTypeNewConn && len(parts) >= 3 {
		// Rejoin parts[2:] with ":" to handle addresses like "192.168.1.1:12345"
		payload = bytes.Join(parts[2:], []byte(":"))
	}

	return msgType, id, payload, nil
}

// ============================================
// MuxForwarder Interface
// ============================================

// MuxForwarder defines the interface for multiplexed forwarding.
type MuxForwarder interface {
	// ForwardMux forwards data from localConn to muxConn with protocol encapsulation.
	// This is a one-way forward (local -> remote).
	ForwardMux(ctx context.Context, localConn net.Conn, muxConn io.Writer, connID string, encoder MuxEncoder) error
}

// ============================================
// MuxForwarder Implementation
// ============================================

type muxForwarder struct {
	bufferSize int
}

// NewMuxForwarder creates a new multiplexed forwarder.
func NewMuxForwarder() MuxForwarder {
	return &muxForwarder{
		bufferSize: 32 * 1024,
	}
}

// ForwardMux forwards data with protocol encapsulation.
func (f *muxForwarder) ForwardMux(ctx context.Context, localConn net.Conn, muxConn io.Writer, connID string, encoder MuxEncoder) error {
	buf := make([]byte, f.bufferSize)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := localConn.Read(buf)
		if err != nil {
			if err == io.EOF || IsClosedErr(err) {
				// Send close message
				closeMsg, _ := encoder.EncodeClose(connID)
				muxConn.Write(closeMsg)
				return nil
			}
			return err
		}

		if n == 0 {
			continue
		}

		// Encode and send
		encoded, err := encoder.EncodeData(connID, buf[:n])
		if err != nil {
			return err
		}

		_, err = muxConn.Write(encoded)
		if err != nil {
			return err
		}
	}
}

// ============================================
// Bidirectional MuxForwarder
// ============================================

// BidirectionalMuxForwarder defines the interface for bidirectional multiplexed forwarding.
type BidirectionalMuxForwarder interface {
	// ForwardBidirectionalMux forwards data in both directions with protocol encapsulation.
	ForwardBidirectionalMux(
		ctx context.Context,
		localConn net.Conn,
		muxConn io.ReadWriter,
		connID string,
		encoder MuxEncoder,
		decoder MuxDecoder,
	) error
}

type bidirectionalMuxForwarder struct {
	bufferSize int
}

// NewBidirectionalMuxForwarder creates a new bidirectional multiplexed forwarder.
func NewBidirectionalMuxForwarder() BidirectionalMuxForwarder {
	return &bidirectionalMuxForwarder{
		bufferSize: 32 * 1024,
	}
}

// ForwardBidirectionalMux forwards data in both directions.
func (f *bidirectionalMuxForwarder) ForwardBidirectionalMux(
	ctx context.Context,
	localConn net.Conn,
	muxConn io.ReadWriter,
	connID string,
	encoder MuxEncoder,
	decoder MuxDecoder,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)

	// Local -> Remote
	go func() {
		buf := make([]byte, f.bufferSize)
		for {
			select {
			case <-ctx.Done():
				errCh <- nil
				return
			default:
			}

			n, err := localConn.Read(buf)
			if err != nil {
				if err == io.EOF || IsClosedErr(err) {
					closeMsg, _ := encoder.EncodeClose(connID)
					muxConn.Write(closeMsg)
					errCh <- nil
					return
				}
				errCh <- err
				return
			}

			if n > 0 {
				encoded, _ := encoder.EncodeData(connID, buf[:n])
				if _, err := muxConn.Write(encoded); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	// Remote -> Local
	go func() {
		buf := make([]byte, f.bufferSize)
		for {
			select {
			case <-ctx.Done():
				errCh <- nil
				return
			default:
			}

			n, err := muxConn.Read(buf)
			if err != nil {
				if err == io.EOF || IsClosedErr(err) {
					errCh <- nil
					return
				}
				errCh <- err
				return
			}

			if n > 0 {
				// Decode message
				msgType, _, payload, err := decoder.Decode(buf[:n])
				if err != nil {
					continue
				}

				switch msgType {
				case MsgTypeData, MsgTypeResponse:
					if len(payload) > 0 {
						localConn.Write(payload)
					}
				case MsgTypeClose:
					localConn.Close()
					errCh <- nil
					return
				}
			}
		}
	}()

	return <-errCh
}

// ============================================
// Mux Connection Manager
// ============================================

// MuxConnManager manages multiple connections over a shared multiplexed connection.
type MuxConnManager struct {
	// connections maps connID -> localConn
	connections sync.Map

	// encoder for encoding messages
	encoder MuxEncoder

	// decoder for decoding messages
	decoder MuxDecoder

	// muxConn is the shared multiplexed connection
	muxConn io.ReadWriter

	// forwarder for multiplexed forwarding
	forwarder MuxForwarder

	// context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// stats
	activeConns atomic.Int64
	totalConns  atomic.Int64
	bytesIn     atomic.Int64
	bytesOut    atomic.Int64
}

// NewMuxConnManager creates a new multiplexed connection manager.
func NewMuxConnManager(muxConn io.ReadWriter, encoder MuxEncoder, decoder MuxDecoder) *MuxConnManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &MuxConnManager{
		encoder:   encoder,
		decoder:   decoder,
		muxConn:   muxConn,
		forwarder: NewMuxForwarder(),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// AddConnection adds a new connection and starts forwarding.
func (m *MuxConnManager) AddConnection(connID string, localConn net.Conn) {
	m.connections.Store(connID, localConn)
	m.activeConns.Add(1)
	m.totalConns.Add(1)

	// Start forwarding
	go func() {
		m.forwarder.ForwardMux(m.ctx, localConn, m.muxConn, connID, m.encoder)
		m.RemoveConnection(connID)
	}()
}

// RemoveConnection removes a connection.
func (m *MuxConnManager) RemoveConnection(connID string) {
	if conn, ok := m.connections.LoadAndDelete(connID); ok {
		conn.(net.Conn).Close()
		m.activeConns.Add(-1)
	}
}

// GetConnection gets a connection by ID.
func (m *MuxConnManager) GetConnection(connID string) (net.Conn, bool) {
	conn, ok := m.connections.Load(connID)
	if !ok {
		return nil, false
	}
	return conn.(net.Conn), true
}

// HandleIncoming handles an incoming message from the mux connection.
func (m *MuxConnManager) HandleIncoming(data []byte) error {
	msgType, id, payload, err := m.decoder.Decode(data)
	if err != nil {
		return err
	}

	switch msgType {
	case MsgTypeData:
		if conn, ok := m.GetConnection(id); ok {
			n, _ := conn.Write(payload)
			m.bytesIn.Add(int64(n))
		}

	case MsgTypeClose:
		m.RemoveConnection(id)

	case MsgTypeResponse:
		if conn, ok := m.GetConnection(id); ok {
			n, _ := conn.Write(payload)
			m.bytesIn.Add(int64(n))
		}

	case MsgTypeNewConn:
		// New connection notification, handled by upper layer
		return NewConnNotification{ConnID: id, RemoteAddr: string(payload)}
	}

	return nil
}

// NewConnNotification is returned when a new connection notification is received.
type NewConnNotification struct {
	ConnID     string
	RemoteAddr string
}

// Error implements the error interface.
func (n NewConnNotification) Error() string {
	return fmt.Sprintf("new connection: %s from %s", n.ConnID, n.RemoteAddr)
}

// Close closes the manager and all connections.
func (m *MuxConnManager) Close() {
	m.cancel()

	m.connections.Range(func(key, value interface{}) bool {
		value.(net.Conn).Close()
		return true
	})
}

// Stats returns the current statistics.
func (m *MuxConnManager) Stats() MuxConnStats {
	return MuxConnStats{
		ActiveConnections: m.activeConns.Load(),
		TotalConnections:  m.totalConns.Load(),
		BytesIn:           m.bytesIn.Load(),
		BytesOut:          m.bytesOut.Load(),
	}
}

// MuxConnStats holds statistics for the mux connection manager.
type MuxConnStats struct {
	ActiveConnections int64
	TotalConnections  int64
	BytesIn           int64
	BytesOut          int64
}

// ============================================
// HTTP Mux Support
// ============================================

// HTTPMuxForwarder handles HTTP request-response forwarding over a multiplexed connection.
type HTTPMuxForwarder interface {
	// ForwardHTTP forwards an HTTP request and returns the response.
	ForwardHTTP(ctx context.Context, reqData []byte, muxConn io.ReadWriter, reqID string, encoder MuxEncoder, decoder MuxDecoder, timeout time.Duration) ([]byte, error)
}

type httpMuxForwarder struct{}

// NewHTTPMuxForwarder creates a new HTTP multiplexed forwarder.
func NewHTTPMuxForwarder() HTTPMuxForwarder {
	return &httpMuxForwarder{}
}

// ForwardHTTP forwards an HTTP request and returns the response.
func (f *httpMuxForwarder) ForwardHTTP(ctx context.Context, reqData []byte, muxConn io.ReadWriter, reqID string, encoder MuxEncoder, decoder MuxDecoder, timeout time.Duration) ([]byte, error) {
	// Encode and send request
	encoded, err := encoder.EncodeRequest(reqID, reqData)
	if err != nil {
		return nil, err
	}

	if _, err := muxConn.Write(encoded); err != nil {
		return nil, err
	}

	// Wait for response
	if timeout > 0 {
		deadline := time.Now().Add(timeout)
		if conn, ok := muxConn.(interface{ SetReadDeadline(time.Time) error }); ok {
			conn.SetReadDeadline(deadline)
		}
	}

	respBuf := make([]byte, 64*1024)
	n, err := muxConn.Read(respBuf)
	if err != nil {
		return nil, err
	}

	// Decode response
	msgType, id, payload, err := decoder.Decode(respBuf[:n])
	if err != nil {
		return nil, err
	}

	if msgType != MsgTypeResponse {
		return nil, fmt.Errorf("expected RESPONSE, got %s", msgType)
	}

	if id != reqID {
		return nil, fmt.Errorf("response ID mismatch: expected %s, got %s", reqID, id)
	}

	return payload, nil
}
