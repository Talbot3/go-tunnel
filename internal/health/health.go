// Package health provides health check endpoints for high availability systems.
package health

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"
)

// Status represents the health status.
type Status string

const (
	// StatusHealthy indicates the component is healthy.
	StatusHealthy Status = "healthy"
	// StatusUnhealthy indicates the component is unhealthy.
	StatusUnhealthy Status = "unhealthy"
	// StatusDegraded indicates the component is degraded but functional.
	StatusDegraded Status = "degraded"
)

// CheckResult represents the result of a health check.
type CheckResult struct {
	Name      string                 `json:"name"`
	Status    Status                 `json:"status"`
	Message   string                 `json:"message,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Duration  time.Duration          `json:"duration"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// CheckFunc is a function that performs a health check.
type CheckFunc func(ctx context.Context) CheckResult

// Checker represents a named health check.
type Checker struct {
	Name     string
	Check    CheckFunc
	Timeout  time.Duration
	Interval time.Duration // For background checks
}

// Health represents the overall health status.
type Health struct {
	Status    Status        `json:"status"`
	Timestamp time.Time     `json:"timestamp"`
	Checks    []CheckResult `json:"checks,omitempty"`
}

// Handler manages health checks.
type Handler struct {
	checkers map[string]*Checker
	results  map[string]CheckResult
	mu       sync.RWMutex

	// Configuration
	timeout time.Duration

	// Background update
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// HandlerConfig holds configuration for the health handler.
type HandlerConfig struct {
	// Timeout for individual health checks.
	// Default: 5 seconds
	Timeout time.Duration

	// Background check interval.
	// If 0, checks are run on-demand only.
	BackgroundInterval time.Duration
}

// NewHandler creates a new health check handler.
func NewHandler(config HandlerConfig) *Handler {
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Handler{
		checkers: make(map[string]*Checker),
		results:  make(map[string]CheckResult),
		timeout:  config.Timeout,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Register registers a new health check.
func (h *Handler) Register(name string, check CheckFunc, opts ...CheckerOption) {
	h.mu.Lock()
	defer h.mu.Unlock()

	checker := &Checker{
		Name:    name,
		Check:   check,
		Timeout: h.timeout,
	}

	for _, opt := range opts {
		opt(checker)
	}

	h.checkers[name] = checker
}

// CheckerOption configures a health checker.
type CheckerOption func(*Checker)

// WithTimeout sets the timeout for the health check.
func WithTimeout(timeout time.Duration) CheckerOption {
	return func(c *Checker) {
		c.Timeout = timeout
	}
}

// WithInterval sets the interval for background health checks.
func WithInterval(interval time.Duration) CheckerOption {
	return func(c *Checker) {
		c.Interval = interval
	}
}

// RunChecks runs all registered health checks and returns the results.
func (h *Handler) RunChecks(ctx context.Context) Health {
	h.mu.RLock()
	checkers := make([]*Checker, 0, len(h.checkers))
	for _, c := range h.checkers {
		checkers = append(checkers, c)
	}
	h.mu.RUnlock()

	results := make([]CheckResult, 0, len(checkers))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, checker := range checkers {
		wg.Add(1)
		go func(c *Checker) {
			defer wg.Done()

			result := h.runCheck(ctx, c)

			mu.Lock()
			results = append(results, result)
			mu.Unlock()
		}(checker)
	}

	wg.Wait()

	// Determine overall status
	overallStatus := StatusHealthy
	for _, r := range results {
		if r.Status == StatusUnhealthy {
			overallStatus = StatusUnhealthy
			break
		}
		if r.Status == StatusDegraded && overallStatus != StatusUnhealthy {
			overallStatus = StatusDegraded
		}
	}

	return Health{
		Status:    overallStatus,
		Timestamp: time.Now(),
		Checks:    results,
	}
}

// runCheck runs a single health check with timeout.
func (h *Handler) runCheck(ctx context.Context, checker *Checker) CheckResult {
	start := time.Now()

	// Create timeout context
	checkCtx, cancel := context.WithTimeout(ctx, checker.Timeout)
	defer cancel()

	// Run check in goroutine to handle timeout
	resultCh := make(chan CheckResult, 1)
	go func() {
		resultCh <- checker.Check(checkCtx)
	}()

	var result CheckResult
	select {
	case r := <-resultCh:
		result = r
	case <-checkCtx.Done():
		result = CheckResult{
			Name:      checker.Name,
			Status:    StatusUnhealthy,
			Message:   "health check timed out",
			Timestamp: time.Now(),
		}
	}

	result.Duration = time.Since(start)
	if result.Timestamp.IsZero() {
		result.Timestamp = time.Now()
	}

	// Store result
	h.mu.Lock()
	h.results[checker.Name] = result
	h.mu.Unlock()

	return result
}

// GetResult returns the last result for a specific check.
func (h *Handler) GetResult(name string) (CheckResult, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result, ok := h.results[name]
	return result, ok
}

// StartBackground starts background health checks.
func (h *Handler) StartBackground(interval time.Duration) {
	if interval <= 0 {
		return
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-h.ctx.Done():
				return
			case <-ticker.C:
				h.RunChecks(h.ctx)
			}
		}
	}()
}

// Stop stops the health handler.
func (h *Handler) Stop() {
	h.cancel()
	h.wg.Wait()
}

// ServeHTTP implements http.Handler for the health endpoint.
// Returns 200 if healthy, 503 if unhealthy.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	health := h.RunChecks(r.Context())

	statusCode := http.StatusOK
	if health.Status == StatusUnhealthy {
		statusCode = http.StatusServiceUnavailable
	} else if health.Status == StatusDegraded {
		statusCode = http.StatusOK // Degraded is still OK
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(health)
}

// LivenessHandler returns a simple liveness handler.
// Returns 200 if the service is running.
func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "alive",
		})
	}
}

// ReadinessHandler returns a readiness handler that checks all dependencies.
func ReadinessHandler(h *Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		health := h.RunChecks(r.Context())

		statusCode := http.StatusOK
		if health.Status == StatusUnhealthy {
			statusCode = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(health)
	}
}

// Common health checks

// PingCheck returns a simple ping health check.
func PingCheck(name string) *Checker {
	return &Checker{
		Name: name,
		Check: func(ctx context.Context) CheckResult {
			return CheckResult{
				Name:      name,
				Status:    StatusHealthy,
				Message:   "pong",
				Timestamp: time.Now(),
			}
		},
	}
}

// TCPCheck returns a TCP health check.
func TCPCheck(name, addr string, timeout time.Duration) *Checker {
	return &Checker{
		Name:    name,
		Timeout: timeout,
		Check: func(ctx context.Context) CheckResult {
			dialer := &net.Dialer{Timeout: timeout}
			conn, err := dialer.DialContext(ctx, "tcp", addr)
			if err != nil {
				return CheckResult{
					Name:      name,
					Status:    StatusUnhealthy,
					Message:   err.Error(),
					Timestamp: time.Now(),
				}
			}
			conn.Close()

			return CheckResult{
				Name:      name,
				Status:    StatusHealthy,
				Timestamp: time.Now(),
			}
		},
	}
}

// HTTPCheck returns an HTTP health check.
func HTTPCheck(name, url string, timeout time.Duration) *Checker {
	return &Checker{
		Name:    name,
		Timeout: timeout,
		Check: func(ctx context.Context) CheckResult {
			client := &http.Client{Timeout: timeout}
			resp, err := client.Get(url)
			if err != nil {
				return CheckResult{
					Name:      name,
					Status:    StatusUnhealthy,
					Message:   err.Error(),
					Timestamp: time.Now(),
				}
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 500 {
				return CheckResult{
					Name:      name,
					Status:    StatusUnhealthy,
					Message:   resp.Status,
					Timestamp: time.Now(),
				}
			}

			if resp.StatusCode >= 400 {
				return CheckResult{
					Name:      name,
					Status:    StatusDegraded,
					Message:   resp.Status,
					Timestamp: time.Now(),
				}
			}

			return CheckResult{
				Name:      name,
				Status:    StatusHealthy,
				Timestamp: time.Now(),
			}
		},
	}
}
