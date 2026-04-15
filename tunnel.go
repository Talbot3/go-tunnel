// Package tunnel provides a high-performance, cross-platform data forwarding library.
//
// The tunnel package implements optimized data forwarding with support for multiple
// protocols including TCP, HTTP/2, HTTP/3, and QUIC. It uses platform-specific
// optimizations for maximum performance:
//
//   - Linux: Zero-copy using unix.Splice syscall
//   - macOS: TCP_NOTSENT_LOWAT and TCP_NODELAY optimizations
//   - Windows: IOCP with large buffer optimization
//
// # Basic Usage
//
// Create a simple TCP forwarder:
//
//	listener, _ := tunnel.Listen("tcp", ":8080")
//	for {
//	    src, _ := listener.Accept()
//	    dst, _ := tunnel.Dial("tcp", "127.0.0.1:80")
//	    go tunnel.Forward(src, dst)
//	}
//
// # Using Tunnel Instance
//
// For more control, create a Tunnel instance:
//
//	cfg := tunnel.Config{
//	    Protocol:   "tcp",
//	    ListenAddr: ":8080",
//	    TargetAddr: "127.0.0.1:80",
//	}
//	t, _ := tunnel.New(cfg)
//	t.Start(context.Background())
//	defer t.Stop()
//
// # Protocol Support
//
// The library supports multiple protocols:
//
//	// TCP
//	p := tcp.New()
//
//	// HTTP/2
//	p := http2.New(tlsConfig)
//
//	// HTTP/3
//	p := http3.New(tlsConfig, nil)
//
//	// QUIC (multiplexing)
//	server := quic.NewMuxServer(quic.MuxServerConfig{TLSConfig: tlsConfig})
//	client := quic.NewMuxClient(quic.MuxClientConfig{ServerAddr: "server:443", TLSConfig: tlsConfig})
package tunnel

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Talbot3/go-tunnel/forward"
	"github.com/Talbot3/go-tunnel/internal/backpressure"
	"github.com/Talbot3/go-tunnel/internal/pool"
)

// Version returns the library version.
const Version = "1.1.0"

// Forwarder defines the interface for bidirectional data forwarding.
type Forwarder interface {
	// Forward copies data bidirectionally between src and dst.
	// It returns when either connection is closed or an error occurs.
	Forward(src, dst net.Conn) error
}

// Protocol defines the interface for protocol-specific handlers.
type Protocol interface {
	// Name returns the protocol name (e.g., "tcp", "http2", "http3", "quic").
	Name() string

	// Listen creates a listener on the specified address.
	Listen(addr string) (net.Listener, error)

	// Dial connects to the specified address.
	Dial(ctx context.Context, addr string) (net.Conn, error)

	// Forwarder returns the forwarder for this protocol.
	Forwarder() forward.Forwarder
}

// Config holds the configuration for a tunnel instance.
type Config struct {
	// Protocol specifies the protocol to use (tcp, http2, http3, quic).
	// Default is "tcp".
	Protocol string

	// ListenAddr is the address to listen on (e.g., ":8080").
	ListenAddr string

	// TargetAddr is the address to forward to (e.g., "127.0.0.1:80").
	TargetAddr string

	// TLSConfig is the TLS configuration for secure protocols.
	// Required for http2, http3, and quic protocols.
	TLSConfig *tls.Config

	// BufferSize is the buffer size for data transfer.
	// Default is 64KB. Use BufferSizeLarge (256KB) for high throughput.
	BufferSize int

	// EnableBackpressure enables backpressure control to prevent
	// memory overflow under high load. Default is true.
	EnableBackpressure bool

	// === 运行模式 ===

	// Mode specifies the operation mode (server/client).
	// Default is ModeAuto which auto-detects based on usage.
	Mode Mode

	// === 连接管理（服务器场景）===

	// MaxConnections is the maximum number of concurrent connections.
	// 0 means unlimited. Use for server mode to prevent resource exhaustion.
	MaxConnections int

	// ConnectionTimeout is the idle timeout for connections.
	// 0 means no timeout. Useful for server mode.
	ConnectionTimeout time.Duration

	// AcceptTimeout is the timeout for accepting new connections.
	AcceptTimeout time.Duration

	// === 数据传输优化 ===

	// ReadBufferSize is the size of read buffer.
	// Default is BufferSize if not set.
	ReadBufferSize int

	// WriteBufferSize is the size of write buffer.
	// Useful when response data is larger than request data.
	WriteBufferSize int

	// WriteBufferPool enables write buffer pooling.
	WriteBufferPool bool

	// === 背压控制 ===

	// BackpressureHighWatermark is the high watermark for backpressure.
	// Default is 1MB for client, 2MB for server.
	BackpressureHighWatermark int

	// BackpressureLowWatermark is the low watermark for backpressure.
	// Default is 512KB for client, 1MB for server.
	BackpressureLowWatermark int

	// BackpressureYieldMin is the minimum yield time for backpressure.
	// Default is 50 microseconds.
	BackpressureYieldMin time.Duration

	// BackpressureYieldMax is the maximum yield time for backpressure.
	// Default is 10 milliseconds (exponential backoff).
	BackpressureYieldMax time.Duration

	// === TCP 优化 ===

	// TCPNoDelay enables TCP_NODELAY. Default is true.
	TCPNoDelay bool

	// TCPQuickAck enables TCP_QUICKACK on Linux. Default is false.
	TCPQuickAck bool

	// TCPFastOpen enables TCP_FASTOPEN. Default is false.
	TCPFastOpen bool

	// SendBufferSize sets SO_SNDBUF. 0 means use system default.
	SendBufferSize int

	// RecvBufferSize sets SO_RCVBUF. 0 means use system default.
	RecvBufferSize int

	// === 监控 ===

	// EnableMetrics enables Prometheus metrics.
	EnableMetrics bool

	// MetricsPrefix is the prefix for Prometheus metrics.
	MetricsPrefix string
}

// Mode specifies the operation mode for the tunnel.
type Mode int

const (
	// ModeAuto auto-detects the mode based on configuration.
	ModeAuto Mode = iota
	// ModeServer is for server-side operation (high concurrency).
	ModeServer
	// ModeClient is for client-side operation (high throughput per connection).
	ModeClient
)

// String returns the string representation of the mode.
func (m Mode) String() string {
	switch m {
	case ModeServer:
		return "server"
	case ModeClient:
		return "client"
	default:
		return "auto"
	}
}

// Tunnel represents an active tunnel instance.
type Tunnel struct {
	config   Config
	protocol Protocol
	listener net.Listener
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stats    *Stats
}

// Stats holds runtime statistics for a tunnel.
type Stats struct {
	connections atomic.Int64
	bytesSent   atomic.Int64
	bytesRecv   atomic.Int64
	errors      atomic.Int64
	startTime   time.Time
}

// Connections returns the total number of connections handled.
func (s *Stats) Connections() int64 {
	return s.connections.Load()
}

// BytesSent returns the total bytes sent from source to target.
func (s *Stats) BytesSent() int64 {
	return s.bytesSent.Load()
}

// BytesReceived returns the total bytes received from target to source.
func (s *Stats) BytesReceived() int64 {
	return s.bytesRecv.Load()
}

// Errors returns the total number of errors encountered.
func (s *Stats) Errors() int64 {
	return s.errors.Load()
}

// Uptime returns how long the tunnel has been running.
func (s *Stats) Uptime() time.Duration {
	return time.Since(s.startTime)
}

// Reset resets all statistics to zero and updates the start time.
func (s *Stats) Reset() {
	s.connections.Store(0)
	s.bytesSent.Store(0)
	s.bytesRecv.Store(0)
	s.errors.Store(0)
	s.startTime = time.Now()
}

// New creates a new Tunnel with the given configuration.
// The tunnel is not started until Start() is called.
//
// Example:
//
//	cfg := tunnel.Config{
//	    Protocol:   "tcp",
//	    ListenAddr: ":8080",
//	    TargetAddr: "127.0.0.1:80",
//	}
//	t, err := tunnel.New(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
func New(cfg Config) (*Tunnel, error) {
	return NewWithContext(context.Background(), cfg)
}

// NewWithContext creates a new Tunnel with the given context and configuration.
// The context can be used to cancel the tunnel creation if it takes too long.
func NewWithContext(ctx context.Context, cfg Config) (*Tunnel, error) {
	// Validate configuration
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &Tunnel{
		config: cfg,
		stats: &Stats{
			startTime: time.Now(),
		},
	}, nil
}

// validateConfig validates and sets defaults for the configuration.
func validateConfig(cfg *Config) error {
	// Validate BufferSize
	if cfg.BufferSize < 0 {
		return fmt.Errorf("BufferSize must be non-negative, got %d", cfg.BufferSize)
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = pool.DefaultBufferSize
	}

	// Validate Protocol
	if cfg.Protocol == "" {
		cfg.Protocol = "tcp"
	}

	// Validate MaxConnections
	if cfg.MaxConnections < 0 {
		return fmt.Errorf("MaxConnections must be non-negative, got %d", cfg.MaxConnections)
	}

	// Validate buffer sizes
	if cfg.ReadBufferSize < 0 {
		return fmt.Errorf("ReadBufferSize must be non-negative, got %d", cfg.ReadBufferSize)
	}
	if cfg.WriteBufferSize < 0 {
		return fmt.Errorf("WriteBufferSize must be non-negative, got %d", cfg.WriteBufferSize)
	}

	// Validate backpressure watermarks
	if cfg.BackpressureHighWatermark < 0 {
		return fmt.Errorf("BackpressureHighWatermark must be non-negative, got %d", cfg.BackpressureHighWatermark)
	}
	if cfg.BackpressureLowWatermark < 0 {
		return fmt.Errorf("BackpressureLowWatermark must be non-negative, got %d", cfg.BackpressureLowWatermark)
	}
	if cfg.BackpressureHighWatermark > 0 && cfg.BackpressureLowWatermark > 0 {
		if cfg.BackpressureLowWatermark >= cfg.BackpressureHighWatermark {
			return fmt.Errorf("BackpressureLowWatermark (%d) must be less than BackpressureHighWatermark (%d)",
				cfg.BackpressureLowWatermark, cfg.BackpressureHighWatermark)
		}
	}

	return nil
}

// SetProtocol sets a custom protocol handler for the tunnel.
// This must be called before Start().
func (t *Tunnel) SetProtocol(p Protocol) {
	t.protocol = p
}

// Start starts the tunnel listener and begins accepting connections.
// It returns immediately; the tunnel runs in the background.
func (t *Tunnel) Start(ctx context.Context) error {
	ctx, t.cancel = context.WithCancel(ctx)

	if t.protocol == nil {
		return ErrUnknownProtocol
	}

	listener, err := t.protocol.Listen(t.config.ListenAddr)
	if err != nil {
		return err
	}
	t.listener = listener

	t.wg.Add(1)
	go t.acceptLoop(ctx)

	return nil
}

// Stop gracefully stops the tunnel.
// It waits for all active connections to complete.
func (t *Tunnel) Stop() error {
	if t.cancel != nil {
		t.cancel()
	}
	if t.listener != nil {
		t.listener.Close()
	}
	t.wg.Wait()
	return nil
}

// Stats returns the current tunnel statistics.
// The returned Stats object is safe for concurrent use.
func (t *Tunnel) Stats() *Stats {
	return t.stats
}

// Addr returns the listener address.
// Returns nil if the tunnel hasn't been started.
func (t *Tunnel) Addr() net.Addr {
	if t.listener == nil {
		return nil
	}
	return t.listener.Addr()
}

func (t *Tunnel) acceptLoop(ctx context.Context) {
	defer t.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		src, err := t.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				t.stats.errors.Add(1)
				continue
			}
		}

		t.stats.connections.Add(1)

		go t.handleConnection(ctx, src)
	}
}

func (t *Tunnel) handleConnection(ctx context.Context, src net.Conn) {
	// Set connection deadline if configured
	if t.config.ConnectionTimeout > 0 {
		src.SetDeadline(time.Now().Add(t.config.ConnectionTimeout))
	}

	dst, err := t.protocol.Dial(ctx, t.config.TargetAddr)
	if err != nil {
		src.Close()
		t.stats.errors.Add(1)
		return
	}

	// Set deadline on destination connection too
	if t.config.ConnectionTimeout > 0 {
		dst.SetDeadline(time.Now().Add(t.config.ConnectionTimeout))
	}

	fwd := t.protocol.Forwarder()

	done := make(chan struct{}, 2)

	// Forward src -> dst
	go func() {
		defer func() { done <- struct{}{} }()
		if err := fwd.Forward(src, dst); err != nil {
			if !IsClosedErr(err) {
				t.stats.errors.Add(1)
			}
		}
	}()

	// Forward dst -> src
	go func() {
		defer func() { done <- struct{}{} }()
		if err := fwd.Forward(dst, src); err != nil {
			if !IsClosedErr(err) {
				t.stats.errors.Add(1)
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}

	src.Close()
	dst.Close()
}

// IsClosedErr returns true if the error indicates a normal connection close.
// This includes EOF, connection reset, and use of closed network connection.
func IsClosedErr(err error) bool {
	if err == nil {
		return true
	}
	if err == io.EOF {
		return true
	}
	if opErr, ok := err.(*net.OpError); ok {
		return opErr.Err.Error() == "use of closed network connection"
	}
	switch err {
	case syscall.EPIPE, syscall.ECONNRESET, syscall.ECONNABORTED:
		return true
	}
	return false
}

// Forward performs bidirectional forwarding between two connections.
// This is a convenience function that creates a default forwarder.
// For more control, use HandlePair or create a Forwarder directly.
//
// Example:
//
//	go tunnel.Forward(clientConn, targetConn)
func Forward(src, dst net.Conn) error {
	fwd := NewForwarder()
	var wg sync.WaitGroup
	wg.Add(2)

	var err1, err2 error

	go func() {
		defer wg.Done()
		err1 = fwd.Forward(src, dst)
	}()

	go func() {
		defer wg.Done()
		err2 = fwd.Forward(dst, src)
	}()

	wg.Wait()

	if err1 != nil && !IsClosedErr(err1) {
		return err1
	}
	if err2 != nil && !IsClosedErr(err2) {
		return err2
	}
	return nil
}

// HandlePair starts bidirectional forwarding between two connections
// and waits for both directions to complete. It closes both connections
// when done.
//
// Example:
//
//	go tunnel.HandlePair(clientConn, targetConn)
func HandlePair(connA, connB net.Conn) {
	fwd := NewForwarder()
	var wg sync.WaitGroup
	wg.Add(2)

	bp := backpressure.NewPair()

	go func() {
		defer wg.Done()
		if err := fwd.Forward(connA, connB); err != nil {
			bp.SignalErrorA()
		}
	}()

	go func() {
		defer wg.Done()
		if err := fwd.Forward(connB, connA); err != nil {
			bp.SignalErrorB()
		}
	}()

	wg.Wait()
	connA.Close()
	connB.Close()
}

// Listen creates a listener using the default TCP protocol.
// This is a convenience wrapper around net.Listen.
func Listen(network, addr string) (net.Listener, error) {
	return net.Listen(network, addr)
}

// Dial connects to an address using the default TCP protocol.
// This is a convenience wrapper around net.Dial.
func Dial(network, addr string) (net.Conn, error) {
	return net.Dial(network, addr)
}

// DialContext connects to an address with context using the default TCP protocol.
// This is a convenience wrapper around net.Dialer.DialContext.
func DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

// NewForwarder creates a platform-optimized forwarder.
// The returned forwarder uses the best available optimization for the
// current platform (splice on Linux, optimized copy on macOS/Windows).
func NewForwarder() Forwarder {
	return &defaultForwarder{}
}

// defaultForwarder wraps forward.Forwarder to implement tunnel.Forwarder.
type defaultForwarder struct{}

func (f *defaultForwarder) Forward(src, dst net.Conn) error {
	fwd := forward.NewForwarder()
	return fwd.Forward(src, dst)
}

// OptimizeTCPConn applies common TCP optimizations to a connection.
// This includes enabling TCP_NODELAY and keep-alive.
func OptimizeTCPConn(conn *net.TCPConn) error {
	if conn == nil {
		return nil
	}
	if err := conn.SetNoDelay(true); err != nil {
		return err
	}
	if err := conn.SetKeepAlive(true); err != nil {
		return err
	}
	return conn.SetKeepAlivePeriod(30 * time.Second)
}

// Buffer sizes for different use cases.
const (
	// BufferSizeDefault is the default buffer size (64KB).
	BufferSizeDefault = pool.DefaultBufferSize

	// BufferSizeLarge is for high-throughput scenarios (256KB).
	BufferSizeLarge = pool.LargeBufferSize
)

// ServerPreset returns a Config optimized for server scenarios.
// Suitable for handling many concurrent connections (e.g., tunnel server).
func ServerPreset() Config {
	return Config{
		Mode:                      ModeServer,
		BufferSize:                64 * 1024,
		MaxConnections:            10000,
		ConnectionTimeout:         5 * time.Minute,
		EnableBackpressure:        true,
		BackpressureHighWatermark: 2 * 1024 * 1024, // 2MB
		BackpressureLowWatermark:  1 * 1024 * 1024, // 1MB
		BackpressureYieldMin:      50 * time.Microsecond,
		BackpressureYieldMax:      10 * time.Millisecond,
		TCPNoDelay:                true,
		SendBufferSize:            256 * 1024,
		RecvBufferSize:            256 * 1024,
	}
}

// ClientPreset returns a Config optimized for client scenarios.
// Suitable for few connections with high throughput (e.g., tunnel client).
func ClientPreset() Config {
	return Config{
		Mode:                      ModeClient,
		BufferSize:                32 * 1024,
		MaxConnections:            0, // No limit for client
		ConnectionTimeout:         0, // No timeout for long-lived connections
		EnableBackpressure:        true,
		BackpressureHighWatermark: 512 * 1024, // 512KB
		BackpressureLowWatermark:  256 * 1024, // 256KB
		BackpressureYieldMin:      50 * time.Microsecond,
		BackpressureYieldMax:      5 * time.Millisecond,
		TCPNoDelay:                true,
		TCPQuickAck:               true,
		TCPFastOpen:               true,
		SendBufferSize:            128 * 1024,
		RecvBufferSize:            128 * 1024,
	}
}

// HighThroughputPreset returns a Config optimized for high-throughput scenarios.
// Suitable when response data is significantly larger than request data.
func HighThroughputPreset() Config {
	return Config{
		Mode:                      ModeClient,
		BufferSize:                128 * 1024,
		WriteBufferSize:           256 * 1024,
		EnableBackpressure:        true,
		BackpressureHighWatermark: 4 * 1024 * 1024, // 4MB
		BackpressureLowWatermark:  2 * 1024 * 1024, // 2MB
		BackpressureYieldMin:      50 * time.Microsecond,
		BackpressureYieldMax:      10 * time.Millisecond,
		TCPNoDelay:                true,
		SendBufferSize:            512 * 1024,
		RecvBufferSize:            512 * 1024,
	}
}
