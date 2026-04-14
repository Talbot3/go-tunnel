// Package backpressure provides flow control mechanisms for data forwarding.
package backpressure

import (
	"sync"
	"time"
)

// Controller manages backpressure for a data flow.
type Controller struct {
	mu     sync.Mutex
	paused bool

	// Watermark settings
	highWatermark int
	lowWatermark  int
	currentLevel  int
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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.paused = true
}

// Resume allows data flow to continue.
func (c *Controller) Resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.paused = false
}

// IsPaused returns whether the flow is paused.
func (c *Controller) IsPaused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused
}

// CheckAndYield checks if paused and yields CPU time if so.
// Returns true if yielding was performed.
func (c *Controller) CheckAndYield() bool {
	c.mu.Lock()
	paused := c.paused
	c.mu.Unlock()

	if paused {
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
		c.paused = true
	} else if level <= c.lowWatermark {
		c.paused = false
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
