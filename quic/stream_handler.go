package quic

import (
	"context"
	"encoding/binary"
	"io"
	"log"
	"net"
	"sync"

	"github.com/quic-go/quic-go"

	"github.com/Talbot3/go-tunnel/internal/pool"
)

// StreamHandler handles data stream processing for MuxClient.
// It manages incoming data streams from the server and forwards them to local services.
type StreamHandler struct {
	// Connection for accepting streams
	conn func() quic.Connection

	// Local connection tracking
	localConns     *sync.Map // connID -> net.Conn
	localQUICConns *sync.Map // connID -> quic.Connection

	// Buffer pool
	bufferPool *pool.BufferPool

	// Configuration
	localAddr string
	protocol  byte

	// Context
	ctx context.Context

	// Wait group
	wg *sync.WaitGroup

	// Stats callbacks
	onBytesIn  func(n int)
	onBytesOut func(n int)
}

// StreamHandlerConfig holds configuration for StreamHandler.
type StreamHandlerConfig struct {
	// Conn is a function that returns the current QUIC connection.
	Conn func() quic.Connection

	// LocalConns is the map for tracking local TCP connections.
	LocalConns *sync.Map

	// LocalQUICConns is the map for tracking local QUIC connections.
	LocalQUICConns *sync.Map

	// BufferPool is the shared buffer pool.
	BufferPool *pool.BufferPool

	// LocalAddr is the default local service address.
	LocalAddr string

	// Protocol is the tunnel protocol.
	Protocol byte

	// OnBytesIn is called when bytes are received.
	OnBytesIn func(n int)

	// OnBytesOut is called when bytes are sent.
	OnBytesOut func(n int)
}

// NewStreamHandler creates a new stream handler.
func NewStreamHandler(config StreamHandlerConfig) *StreamHandler {
	return &StreamHandler{
		conn:           config.Conn,
		localConns:     config.LocalConns,
		localQUICConns: config.LocalQUICConns,
		bufferPool:     config.BufferPool,
		localAddr:      config.LocalAddr,
		protocol:       config.Protocol,
		onBytesIn:      config.OnBytesIn,
		onBytesOut:     config.OnBytesOut,
	}
}

// SetContext sets the context for the handler.
func (h *StreamHandler) SetContext(ctx context.Context, wg *sync.WaitGroup) {
	h.ctx = ctx
	h.wg = wg
}

// AcceptLoop runs the stream acceptance loop.
// Should be run in a goroutine.
func (h *StreamHandler) AcceptLoop() {
	if h.wg != nil {
		defer h.wg.Done()
	}

	log.Printf("[StreamHandler] Data stream acceptor started for protocol %d", h.protocol)

	for {
		select {
		case <-h.ctx.Done():
			return
		default:
		}

		conn := h.conn()
		if conn == nil {
			return
		}

		// Accept data stream from server
		stream, err := conn.AcceptStream(h.ctx)
		if err != nil {
			if h.ctx.Err() != nil {
				return
			}
			log.Printf("[StreamHandler] Accept data stream failed: %v", err)
			continue
		}

		log.Printf("[StreamHandler] Accepted data stream from server")
		if h.wg != nil {
			h.wg.Add(1)
		}
		go h.HandleDataStream(stream)
	}
}

// HandleDataStream handles a data stream from the server.
// Supports both legacy format and new session header format.
func (h *StreamHandler) HandleDataStream(stream quic.Stream) {
	if h.wg != nil {
		defer h.wg.Done()
	}
	defer stream.Close()

	// Read stream type
	var streamTypeBuf [1]byte
	if _, err := io.ReadFull(stream, streamTypeBuf[:]); err != nil {
		log.Printf("[StreamHandler] Failed to read stream type: %v", err)
		return
	}

	if streamTypeBuf[0] != StreamTypeData {
		log.Printf("[StreamHandler] Expected data stream, got type %d", streamTypeBuf[0])
		return
	}

	// Parse session header
	header, err := h.parseSessionHeader(stream)
	if err != nil {
		log.Printf("[StreamHandler] Failed to parse session header: %v", err)
		return
	}

	// Determine target address
	targetAddr := h.localAddr
	if header.Target != "" {
		targetAddr = header.Target
	}

	// Handle based on protocol type
	switch h.protocol {
	case ProtocolQUIC:
		h.forwardQUICStream(stream, header.ConnID, targetAddr)
	case ProtocolTCP, ProtocolHTTP:
		h.forwardTCPStream(stream, header.ConnID, targetAddr)
	default:
		log.Printf("[StreamHandler] Unsupported protocol: %d", h.protocol)
	}
}

// parseSessionHeader parses the session header from the stream.
func (h *StreamHandler) parseSessionHeader(stream quic.Stream) (*SessionHeader, error) {
	// Read the next byte to determine format
	var nextByte [1]byte
	if _, err := io.ReadFull(stream, nextByte[:]); err != nil {
		return nil, err
	}

	header := &SessionHeader{}

	// Check if this is a protocol type (new format) or connID length high byte (legacy format)
	if nextByte[0] >= ProtocolTCP && nextByte[0] <= ProtocolHTTP3 {
		// New format: read remaining session header
		header.Protocol = nextByte[0]

		// Skip addrType (1 byte)
		var addrTypeBuf [1]byte
		if _, err := io.ReadFull(stream, addrTypeBuf[:]); err != nil {
			return nil, err
		}
		header.AddrType = addrTypeBuf[0]

		var targetLenBuf [2]byte
		if _, err := io.ReadFull(stream, targetLenBuf[:]); err != nil {
			return nil, err
		}
		targetLen := binary.BigEndian.Uint16(targetLenBuf[:])

		// Read target
		if targetLen > 0 {
			targetBytes := make([]byte, targetLen)
			if _, err := io.ReadFull(stream, targetBytes); err != nil {
				return nil, err
			}
			header.Target = string(targetBytes)
		}

		// Read flags
		var flagsBuf [1]byte
		if _, err := io.ReadFull(stream, flagsBuf[:]); err != nil {
			return nil, err
		}
		header.Flags = flagsBuf[0]

		// Read connID length
		var connIDLenBuf [2]byte
		if _, err := io.ReadFull(stream, connIDLenBuf[:]); err != nil {
			return nil, err
		}
		connIDLen := binary.BigEndian.Uint16(connIDLenBuf[:])

		// Read connID
		if connIDLen > 0 {
			connID := make([]byte, connIDLen)
			if _, err := io.ReadFull(stream, connID); err != nil {
				return nil, err
			}
			header.ConnID = string(connID)
		}

		log.Printf("[StreamHandler] Session header: protocol=%d, target=%s, connID=%s",
			header.Protocol, header.Target, header.ConnID)
	} else {
		// Legacy format
		var connIDLenLowBuf [1]byte
		if _, err := io.ReadFull(stream, connIDLenLowBuf[:]); err != nil {
			return nil, err
		}
		connIDLen := uint16(nextByte[0])<<8 | uint16(connIDLenLowBuf[0])

		if connIDLen > 0 {
			connID := make([]byte, connIDLen)
			if _, err := io.ReadFull(stream, connID); err != nil {
				return nil, err
			}
			header.ConnID = string(connID)
		}
		log.Printf("[StreamHandler] Legacy format: connID=%s", header.ConnID)
	}

	return header, nil
}

// forwardTCPStream forwards data between the QUIC stream and a local TCP connection.
func (h *StreamHandler) forwardTCPStream(stream quic.Stream, connID, targetAddr string) {
	// Dial local TCP service
	localConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		log.Printf("[StreamHandler] Dial local TCP failed: %v", err)
		return
	}
	defer localConn.Close()

	// Track connection
	if h.localConns != nil {
		h.localConns.Store(connID, localConn)
		defer h.localConns.Delete(connID)
	}

	// Forward bidirectionally
	forwardBidirectionalSimple(
		h.ctx,
		stream,
		localConn,
		h.bufferPool,
		h.onBytesIn,
		h.onBytesOut,
	)
}

// forwardQUICStream forwards data between the QUIC stream and a local QUIC connection.
func (h *StreamHandler) forwardQUICStream(stream quic.Stream, connID, targetAddr string) {
	// TODO: Implement QUIC forwarding
	// This requires dialing a local QUIC service
	log.Printf("[StreamHandler] QUIC forwarding not yet implemented for connID=%s", connID)
}

// Close closes all local connections.
func (h *StreamHandler) Close() {
	if h.localConns != nil {
		h.localConns.Range(func(key, value interface{}) bool {
			if conn, ok := value.(net.Conn); ok {
				conn.Close()
			}
			return true
		})
	}

	if h.localQUICConns != nil {
		h.localQUICConns.Range(func(key, value interface{}) bool {
			if conn, ok := value.(quic.Connection); ok {
				conn.CloseWithError(0, "stream handler closed")
			}
			return true
		})
	}
}
