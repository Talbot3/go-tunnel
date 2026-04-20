package quic_test

import (
	"sync/atomic"
	"testing"

	tunnelquic "github.com/Talbot3/go-tunnel/quic"
)

// ============================================
// Interface Tests
// ============================================

func TestStatsHandler_Interface(t *testing.T) {
	// Test ServerStatsHandler
	var activeConns, totalConns, bytesIn, bytesOut, activeTunnels, totalTunnels atomic.Int64
	handler := tunnelquic.NewServerStatsHandler(
		&activeConns, &totalConns, &bytesIn, &bytesOut, &activeTunnels, &totalTunnels,
	)

	// Test operations
	handler.AddActiveConnection(1)
	if activeConns.Load() != 1 {
		t.Errorf("Expected activeConns=1, got %d", activeConns.Load())
	}

	handler.AddTotalConnection(1)
	if totalConns.Load() != 1 {
		t.Errorf("Expected totalConns=1, got %d", totalConns.Load())
	}

	handler.AddBytesIn(100)
	if bytesIn.Load() != 100 {
		t.Errorf("Expected bytesIn=100, got %d", bytesIn.Load())
	}

	handler.AddBytesOut(50)
	if bytesOut.Load() != 50 {
		t.Errorf("Expected bytesOut=50, got %d", bytesOut.Load())
	}

	handler.AddActiveTunnel(1)
	if activeTunnels.Load() != 1 {
		t.Errorf("Expected activeTunnels=1, got %d", activeTunnels.Load())
	}

	handler.AddTotalTunnel(1)
	if totalTunnels.Load() != 1 {
		t.Errorf("Expected totalTunnels=1, got %d", totalTunnels.Load())
	}
}

func TestClientStatsHandler_Interface(t *testing.T) {
	// Test ClientStatsHandler
	var activeConns, totalConns, bytesIn, bytesOut atomic.Int64
	handler := tunnelquic.NewClientStatsHandler(
		&activeConns, &totalConns, &bytesIn, &bytesOut,
	)

	// Test operations
	handler.AddActiveConnection(1)
	if activeConns.Load() != 1 {
		t.Errorf("Expected activeConns=1, got %d", activeConns.Load())
	}

	handler.AddBytesIn(100)
	if bytesIn.Load() != 100 {
		t.Errorf("Expected bytesIn=100, got %d", bytesIn.Load())
	}

	// Tunnel operations should be no-op for client
	handler.AddActiveTunnel(1)
	handler.AddTotalTunnel(1)
	// No error means success
}

func TestNoopStatsHandler(t *testing.T) {
	handler := tunnelquic.NewNoopStatsHandler()

	// All operations should succeed without error
	handler.AddActiveConnection(1)
	handler.AddTotalConnection(1)
	handler.AddBytesIn(100)
	handler.AddBytesOut(50)
	handler.AddActiveTunnel(1)
	handler.AddTotalTunnel(1)
	// No error means success
}

// ============================================
// TunnelRegistry Tests
// ============================================

func TestTunnelRegistry_Basic(t *testing.T) {
	portMgr := tunnelquic.NewPortManager(10000, 20000)
	registry := tunnelquic.NewTunnelRegistry(portMgr, nil)

	// Test empty registry
	if count := registry.Count(); count != 0 {
		t.Errorf("Expected count=0, got %d", count)
	}

	// Create test tunnel
	tunnel := &tunnelquic.Tunnel{
		ID:       "test-tunnel-1",
		Protocol: tunnelquic.ProtocolTCP,
		Port:     10001,
	}

	// Register tunnel
	err := registry.Register(tunnel)
	if err != nil {
		t.Fatalf("Failed to register tunnel: %v", err)
	}

	// Verify count
	if count := registry.Count(); count != 1 {
		t.Errorf("Expected count=1, got %d", count)
	}

	// Get tunnel
	retrieved := registry.Get("test-tunnel-1")
	if retrieved == nil {
		t.Error("Expected to retrieve tunnel, got nil")
	}
	if retrieved.ID != tunnel.ID {
		t.Errorf("Expected ID=%s, got %s", tunnel.ID, retrieved.ID)
	}

	// Delete tunnel
	deleted := registry.Delete("test-tunnel-1")
	if deleted == nil {
		t.Error("Expected to delete tunnel, got nil")
	}

	// Verify count after delete
	if count := registry.Count(); count != 0 {
		t.Errorf("Expected count=0 after delete, got %d", count)
	}
}

func TestTunnelRegistry_Domain(t *testing.T) {
	portMgr := tunnelquic.NewPortManager(10000, 20000)
	registry := tunnelquic.NewTunnelRegistry(portMgr, nil)

	// Create tunnel with domain
	tunnel := &tunnelquic.Tunnel{
		ID:       "test-tunnel-domain",
		Protocol: tunnelquic.ProtocolTCP,
		Domain:   "test.example.com",
	}

	// Register tunnel
	registry.Register(tunnel)

	// Get by domain
	retrieved := registry.GetByDomain("test.example.com")
	if retrieved == nil {
		t.Error("Expected to retrieve tunnel by domain, got nil")
	}
	if retrieved.ID != tunnel.ID {
		t.Errorf("Expected ID=%s, got %s", tunnel.ID, retrieved.ID)
	}

	// Delete and verify domain mapping is removed
	registry.Delete("test-tunnel-domain")
	retrieved = registry.GetByDomain("test.example.com")
	if retrieved != nil {
		t.Error("Expected nil after delete, got tunnel")
	}
}

func TestTunnelRegistry_Range(t *testing.T) {
	portMgr := tunnelquic.NewPortManager(10000, 20000)
	registry := tunnelquic.NewTunnelRegistry(portMgr, nil)

	// Register multiple tunnels
	for i := 0; i < 5; i++ {
		tunnel := &tunnelquic.Tunnel{
			ID:       string(rune('a' + i)),
			Protocol: tunnelquic.ProtocolTCP,
		}
		registry.Register(tunnel)
	}

	// Range and count
	count := 0
	registry.Range(func(tunnel *tunnelquic.Tunnel) bool {
		count++
		return true
	})

	if count != 5 {
		t.Errorf("Expected to range over 5 tunnels, got %d", count)
	}

	// Test early termination
	count = 0
	registry.Range(func(tunnel *tunnelquic.Tunnel) bool {
		count++
		return count < 3 // Stop after 3
	})

	if count != 3 {
		t.Errorf("Expected to stop after 3 tunnels, got %d", count)
	}
}

// ============================================
// Protocol Handler Tests
// ============================================

func TestProtocolHandlerRegistry(t *testing.T) {
	registry := tunnelquic.NewProtocolHandlerRegistry(tunnelquic.ProtocolHandlerConfig{})

	// Test TCP handler
	tcpHandler := registry.Get(tunnelquic.ProtocolTCP)
	if tcpHandler == nil {
		t.Error("Expected TCP handler, got nil")
	}
	if tcpHandler.Protocol() != tunnelquic.ProtocolTCP {
		t.Errorf("Expected protocol=%d, got %d", tunnelquic.ProtocolTCP, tcpHandler.Protocol())
	}

	// Test QUIC handler
	quicHandler := registry.Get(tunnelquic.ProtocolQUIC)
	if quicHandler == nil {
		t.Error("Expected QUIC handler, got nil")
	}

	// Test HTTP/3 handler
	h3Handler := registry.Get(tunnelquic.ProtocolHTTP3)
	if h3Handler == nil {
		t.Error("Expected HTTP/3 handler, got nil")
	}

	// Test unknown protocol
	unknownHandler := registry.Get(0xFF)
	if unknownHandler != nil {
		t.Error("Expected nil for unknown protocol")
	}
}

// ============================================
// Session Header Tests
// ============================================

func TestSessionHeader_EncodeDecode(t *testing.T) {
	header := &tunnelquic.SessionHeader{
		Protocol: tunnelquic.ProtocolTCP,
		AddrType: tunnelquic.AddrTypeIPv4,
		Target:   "127.0.0.1:8080",
		Flags:    0,
		ConnID:   "test-conn-123",
	}

	// Encode
	encoded := header.Encode()
	if len(encoded) == 0 {
		t.Error("Expected non-empty encoded header")
	}

	// Verify minimum length
	// 1 (proto) + 1 (addr) + 2 (targetLen) + len(target) + 1 (flags) + 2 (connIDLen) + len(connID)
	expectedMinLen := 1 + 1 + 2 + len(header.Target) + 1 + 2 + len(header.ConnID)
	if len(encoded) < expectedMinLen {
		t.Errorf("Expected encoded length >= %d, got %d", expectedMinLen, len(encoded))
	}
}
