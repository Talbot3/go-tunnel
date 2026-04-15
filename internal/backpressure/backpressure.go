// Package backpressure provides flow control mechanisms for data forwarding.
package backpressure

import (
	"sync"
	"sync/atomic"
	"time"
)

// Controller manages backpressure for a data flow.
type Controller struct {
	mu sync.Mutex

	// Watermark settings
	highWatermark int
	lowWatermark  int
	currentLevel  int

	// Paused state (atomic for fast reads)
	paused atomic.Bool
}

// NewController creates a new backpressure controller.
func NewController() *Controller {
	return &Controller{
		highWatermark: 1 << 20, // 1MB
		lowWatermark:  1 << 19, // 512KB
	}
}

// Pause stops data flow.
func (c *Controller) Pause() {
	c.paused.Store(true)
}

// Resume allows data flow to continue.
func (c *Controller) Resume() {
	c.paused.Store(false)
}

// IsPaused returns whether the flow is paused.
func (c *Controller) IsPaused() bool {
	return c.paused.Load()
}

// CheckAndYield checks if paused and yields CPU time if so.
// Returns true if yielding was performed.
func (c *Controller) CheckAndYield() bool {
	if c.paused.Load() {
		// Yield CPU time
		time.Sleep(50 * time.Microsecond)
		return true
	}
	return false
}

// UpdateLevel updates the current buffer level and manages watermarks.
func (c *Controller) UpdateLevel(level int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentLevel = level

	if level >= c.highWatermark {
		c.paused.Store(true)
	} else if level <= c.lowWatermark {
		c.paused.Store(false)
	}
}

// SetWatermarks configures the high and low watermark levels.
func (c *Controller) SetWatermarks(low, high int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lowWatermark = low
	c.highWatermark = high
}

// Pair manages backpressure for bidirectional data flow.
type Pair struct {
	A *Controller
	B *Controller
}

// NewPair creates a backpressure pair for bidirectional flow.
func NewPair() *Pair {
	return &Pair{
		A: NewController(),
		B: NewController(),
	}
}

// PauseA pauses direction A.
func (p *Pair) PauseA() { p.A.Pause() }

// PauseB pauses direction B.
func (p *Pair) PauseB() { p.B.Pause() }

// ResumeA resumes direction A.
func (p *Pair) ResumeA() { p.A.Resume() }

// ResumeB resumes direction B.
func (p *Pair) ResumeB() { p.B.Resume() }

// SignalErrorA signals an error in direction A, resuming B.
func (p *Pair) SignalErrorA() { p.B.Resume() }

// SignalErrorB signals an error in direction B, resuming A.
func (p *Pair) SignalErrorB() { p.A.Resume() }

// AdaptiveController provides adaptive backpressure with exponential backoff.
// Thread-safe for concurrent use.
type AdaptiveController struct {
	mu sync.Mutex

	// Watermark settings (protected by mu)
	highWatermark int
	lowWatermark  int
	currentLevel  int

	// Paused state
	paused atomic.Bool

	// Exponential backoff settings
	yieldMin     time.Duration
	yieldMax     time.Duration
	yieldCurrent atomic.Int64 // Nanoseconds, uses atomic for lock-free access

	// Statistics
	pauseCount atomic.Int64
	totalPause atomic.Int64
}

// AdaptiveConfig holds configuration for AdaptiveController.
type AdaptiveConfig struct {
	HighWatermark int
	LowWatermark  int
	YieldMin      time.Duration
	YieldMax      time.Duration
}

// NewAdaptiveController creates a new adaptive backpressure controller.
func NewAdaptiveController(cfg AdaptiveConfig) *AdaptiveController {
	if cfg.HighWatermark <= 0 {
		cfg.HighWatermark = 1 << 20 // 1MB
	}
	if cfg.LowWatermark <= 0 {
		cfg.LowWatermark = 1 << 19 // 512KB
	}
	if cfg.YieldMin <= 0 {
		cfg.YieldMin = 50 * time.Microsecond
	}
	if cfg.YieldMax <= 0 {
		cfg.YieldMax = 10 * time.Millisecond
	}

	c := &AdaptiveController{
		highWatermark: cfg.HighWatermark,
		lowWatermark:  cfg.LowWatermark,
		yieldMin:      cfg.YieldMin,
		yieldMax:      cfg.YieldMax,
	}
	c.yieldCurrent.Store(int64(cfg.YieldMin))
	return c
}

// Pause stops data flow.
func (c *AdaptiveController) Pause() {
	c.paused.Store(true)
}

// Resume allows data flow to continue.
func (c *AdaptiveController) Resume() {
	c.paused.Store(false)
	c.yieldCurrent.Store(int64(c.yieldMin))
}

// IsPaused returns whether the flow is paused.
func (c *AdaptiveController) IsPaused() bool {
	return c.paused.Load()
}

// CheckAndYield checks if paused and yields CPU time with exponential backoff.
// Returns true if yielding was performed.
func (c *AdaptiveController) CheckAndYield() bool {
	if !c.paused.Load() {
		return false
	}

	// Lock-free exponential backoff using atomic
	yieldTimeNs := c.yieldCurrent.Load()
	newYieldNs := yieldTimeNs * 2
	maxYieldNs := int64(c.yieldMax)
	if newYieldNs > maxYieldNs {
		newYieldNs = maxYieldNs
	}
	c.yieldCurrent.Store(newYieldNs)

	c.pauseCount.Add(1)
	c.totalPause.Add(yieldTimeNs)

	time.Sleep(time.Duration(yieldTimeNs))
	return true
}

// UpdateLevel updates the current buffer level and manages watermarks.
func (c *AdaptiveController) UpdateLevel(level int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentLevel = level

	if level >= c.highWatermark {
		c.paused.Store(true)
	} else if level <= c.lowWatermark {
		c.paused.Store(false)
		c.yieldCurrent.Store(int64(c.yieldMin))
	}
}

// SetWatermarks configures the high and low watermark levels.
func (c *AdaptiveController) SetWatermarks(low, high int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lowWatermark = low
	c.highWatermark = high
}

// Stats returns backpressure statistics.
func (c *AdaptiveController) Stats() (pauseCount int64, totalPause time.Duration) {
	return c.pauseCount.Load(), time.Duration(c.totalPause.Load())
}

// Reset resets the backoff to minimum.
func (c *AdaptiveController) Reset() {
	c.yieldCurrent.Store(int64(c.yieldMin))
	c.paused.Store(false)
}
