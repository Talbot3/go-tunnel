// Package shutdown provides graceful shutdown mechanisms for high availability.
package shutdown

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"
)

// Handler manages graceful shutdown.
type Handler struct {
	mu sync.Mutex

	// Shutdown timeout
	timeout time.Duration

	// Callbacks to execute during shutdown
	callbacks []Callback

	// Channels
	quit    chan os.Signal
	done    chan struct{}
	stopped bool
}

// Callback represents a shutdown callback.
type Callback struct {
	Name     string
	Priority int // Lower numbers run first
	Func     func(ctx context.Context) error
}

// Config holds shutdown handler configuration.
type Config struct {
	// Timeout is the maximum time to wait for shutdown.
	// Default: 30 seconds
	Timeout time.Duration

	// Signals to listen for shutdown.
	// Default: SIGINT, SIGTERM
	Signals []os.Signal
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Timeout: 30 * time.Second,
		Signals: []os.Signal{os.Interrupt},
	}
}

// NewHandler creates a new shutdown handler.
func NewHandler(config Config) *Handler {
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if len(config.Signals) == 0 {
		config.Signals = []os.Signal{os.Interrupt}
	}

	h := &Handler{
		timeout:   config.Timeout,
		callbacks: make([]Callback, 0),
		quit:      make(chan os.Signal, 1),
		done:      make(chan struct{}),
	}

	signal.Notify(h.quit, config.Signals...)

	return h
}

// Register registers a shutdown callback.
// Lower priority numbers are executed first.
func (h *Handler) Register(name string, priority int, fn func(ctx context.Context) error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.callbacks = append(h.callbacks, Callback{
		Name:     name,
		Priority: priority,
		Func:     fn,
	})
}

// RegisterFunc registers a shutdown callback with default priority (100).
func (h *Handler) RegisterFunc(name string, fn func(ctx context.Context) error) {
	h.Register(name, 100, fn)
}

// Wait blocks until a shutdown signal is received and all callbacks complete.
func (h *Handler) Wait() {
	<-h.quit
	h.Shutdown()
}

// WaitWithContext blocks until shutdown or context cancellation.
func (h *Handler) WaitWithContext(ctx context.Context) error {
	select {
	case <-h.quit:
		h.Shutdown()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Shutdown initiates graceful shutdown.
func (h *Handler) Shutdown() {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return
	}
	h.stopped = true
	h.mu.Unlock()

	log.Println("[Shutdown] Starting graceful shutdown...")

	// Create shutdown context
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()

	// Sort callbacks by priority
	sortedCallbacks := make([]Callback, len(h.callbacks))
	copy(sortedCallbacks, h.callbacks)
	sortCallbacks(sortedCallbacks)

	// Execute callbacks
	var wg sync.WaitGroup
	for _, cb := range sortedCallbacks {
		wg.Add(1)
		go func(callback Callback) {
			defer wg.Done()

			start := time.Now()
			log.Printf("[Shutdown] Running: %s", callback.Name)

			if err := callback.Func(ctx); err != nil {
				log.Printf("[Shutdown] Error in %s: %v", callback.Name, err)
			}

			log.Printf("[Shutdown] Completed: %s (%v)", callback.Name, time.Since(start))
		}(cb)
	}

	// Wait for all callbacks with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("[Shutdown] Graceful shutdown completed")
	case <-ctx.Done():
		log.Println("[Shutdown] Shutdown timeout exceeded, forcing exit")
	}

	close(h.done)
}

// Done returns a channel that's closed when shutdown is complete.
func (h *Handler) Done() <-chan struct{} {
	return h.done
}

// IsShuttingDown returns true if shutdown has been initiated.
func (h *Handler) IsShuttingDown() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopped
}

// sortCallbacks sorts callbacks by priority.
func sortCallbacks(callbacks []Callback) {
	for i := 0; i < len(callbacks)-1; i++ {
		for j := i + 1; j < len(callbacks); j++ {
			if callbacks[i].Priority > callbacks[j].Priority {
				callbacks[i], callbacks[j] = callbacks[j], callbacks[i]
			}
		}
	}
}

// Global shutdown handler for convenience
var (
	globalHandler *Handler
	globalMu      sync.Mutex
)

// Init initializes the global shutdown handler.
func Init(config Config) {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalHandler == nil {
		globalHandler = NewHandler(config)
	}
}

// Register registers a callback with the global handler.
func Register(name string, priority int, fn func(ctx context.Context) error) {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalHandler == nil {
		globalHandler = NewHandler(DefaultConfig())
	}

	globalHandler.Register(name, priority, fn)
}

// RegisterFunc registers a callback with default priority using the global handler.
func RegisterFunc(name string, fn func(ctx context.Context) error) {
	Register(name, 100, fn)
}

// Wait blocks until shutdown using the global handler.
func Wait() {
	globalMu.Lock()
	if globalHandler == nil {
		globalHandler = NewHandler(DefaultConfig())
	}
	h := globalHandler
	globalMu.Unlock()

	h.Wait()
}

// Shutdown initiates shutdown using the global handler.
func Shutdown() {
	globalMu.Lock()
	if globalHandler == nil {
		return
	}
	h := globalHandler
	globalMu.Unlock()

	h.Shutdown()
}

// IsShuttingDown returns true if the global handler is shutting down.
func IsShuttingDown() bool {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalHandler == nil {
		return false
	}
	return globalHandler.IsShuttingDown()
}
