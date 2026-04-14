package backpressure

import (
	"sync"
	"testing"
	"time"
)

func TestController_New(t *testing.T) {
	c := NewController()
	if c == nil {
		t.Fatal("expected non-nil controller")
	}
	if c.IsPaused() {
		t.Error("expected controller to not be paused initially")
	}
}

func TestController_PauseResume(t *testing.T) {
	c := NewController()

	c.Pause()
	if !c.IsPaused() {
		t.Error("expected controller to be paused")
	}

	c.Resume()
	if c.IsPaused() {
		t.Error("expected controller to not be paused")
	}
}

func TestController_CheckAndYield(t *testing.T) {
	c := NewController()

	// Not paused - should not yield
	yielded := c.CheckAndYield()
	if yielded {
		t.Error("expected no yield when not paused")
	}

	// Paused - should yield
	c.Pause()
	start := time.Now()
	yielded = c.CheckAndYield()
	elapsed := time.Since(start)

	if !yielded {
		t.Error("expected yield when paused")
	}
	if elapsed < 40*time.Microsecond {
		t.Errorf("expected yield time >= 50µs, got %v", elapsed)
	}
}

func TestController_UpdateLevel(t *testing.T) {
	c := NewController()
	c.SetWatermarks(100, 200)

	// Below low watermark - not paused
	c.UpdateLevel(50)
	if c.IsPaused() {
		t.Error("expected not paused below low watermark")
	}

	// Above high watermark - paused
	c.UpdateLevel(250)
	if !c.IsPaused() {
		t.Error("expected paused above high watermark")
	}

	// Back to below low watermark - resumed
	c.UpdateLevel(50)
	if c.IsPaused() {
		t.Error("expected not paused below low watermark")
	}
}

func TestController_SetWatermarks(t *testing.T) {
	c := NewController()
	c.SetWatermarks(1000, 2000)

	c.UpdateLevel(1500)
	if c.IsPaused() {
		t.Error("expected not paused at 1500 with high=2000")
	}

	c.UpdateLevel(2500)
	if !c.IsPaused() {
		t.Error("expected paused at 2500 with high=2000")
	}
}

func TestController_Concurrent(t *testing.T) {
	c := NewController()
	var wg sync.WaitGroup

	// Concurrent pause/resume operations
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			c.Pause()
		}()
		go func() {
			defer wg.Done()
			c.Resume()
		}()
	}

	wg.Wait()
	// Just verify no race conditions
}

func TestPair_New(t *testing.T) {
	p := NewPair()
	if p == nil {
		t.Fatal("expected non-nil pair")
	}
	if p.A == nil || p.B == nil {
		t.Fatal("expected non-nil controllers in pair")
	}
}

func TestPair_PauseResume(t *testing.T) {
	p := NewPair()

	p.PauseA()
	if !p.A.IsPaused() {
		t.Error("expected A to be paused")
	}
	if p.B.IsPaused() {
		t.Error("expected B to not be paused")
	}

	p.PauseB()
	if !p.B.IsPaused() {
		t.Error("expected B to be paused")
	}

	p.ResumeA()
	if p.A.IsPaused() {
		t.Error("expected A to not be paused")
	}

	p.ResumeB()
	if p.B.IsPaused() {
		t.Error("expected B to not be paused")
	}
}

func TestPair_SignalError(t *testing.T) {
	p := NewPair()
	p.PauseA()
	p.PauseB()

	// Signal error in A should resume B
	p.SignalErrorA()
	if p.B.IsPaused() {
		t.Error("expected B to be resumed after SignalErrorA")
	}

	// Signal error in B should resume A
	p.SignalErrorB()
	if p.A.IsPaused() {
		t.Error("expected A to be resumed after SignalErrorB")
	}
}

func TestAdaptiveController_New(t *testing.T) {
	c := NewAdaptiveController(AdaptiveConfig{})
	if c == nil {
		t.Fatal("expected non-nil controller")
	}
	if c.IsPaused() {
		t.Error("expected controller to not be paused initially")
	}
}

func TestAdaptiveController_Defaults(t *testing.T) {
	c := NewAdaptiveController(AdaptiveConfig{
		HighWatermark: 0, // Should default to 1MB
		LowWatermark:  0, // Should default to 512KB
		YieldMin:      0, // Should default to 50µs
		YieldMax:      0, // Should default to 10ms
	})

	// Just verify it works with defaults
	c.UpdateLevel(1 << 20) // 1MB
	// Should be paused at high watermark
	if !c.IsPaused() {
		t.Error("expected paused at high watermark")
	}
}

func TestAdaptiveController_PauseResume(t *testing.T) {
	c := NewAdaptiveController(AdaptiveConfig{})

	c.Pause()
	if !c.IsPaused() {
		t.Error("expected controller to be paused")
	}

	c.Resume()
	if c.IsPaused() {
		t.Error("expected controller to not be paused")
	}
}

func TestAdaptiveController_ExponentialBackoff(t *testing.T) {
	c := NewAdaptiveController(AdaptiveConfig{
		YieldMin: 10 * time.Microsecond,
		YieldMax: 1 * time.Millisecond,
	})

	c.Pause()

	// First yield
	start := time.Now()
	c.CheckAndYield()
	elapsed1 := time.Since(start)

	// Second yield should be longer (exponential backoff)
	start = time.Now()
	c.CheckAndYield()
	elapsed2 := time.Since(start)

	// elapsed2 should be approximately 2x elapsed1
	// Allow some tolerance for timing variations
	if elapsed2 < elapsed1 {
		t.Errorf("expected exponential backoff: elapsed1=%v, elapsed2=%v", elapsed1, elapsed2)
	}
}

func TestAdaptiveController_Reset(t *testing.T) {
	c := NewAdaptiveController(AdaptiveConfig{
		YieldMin: 10 * time.Microsecond,
		YieldMax: 1 * time.Millisecond,
	})

	c.Pause()
	c.CheckAndYield() // Increase backoff

	c.Reset()

	if c.IsPaused() {
		t.Error("expected not paused after reset")
	}
}

func TestAdaptiveController_Stats(t *testing.T) {
	c := NewAdaptiveController(AdaptiveConfig{
		YieldMin: 10 * time.Microsecond,
		YieldMax: 100 * time.Microsecond,
	})

	c.Pause()
	c.CheckAndYield()
	c.CheckAndYield()

	pauseCount, totalPause := c.Stats()
	if pauseCount != 2 {
		t.Errorf("expected 2 pause count, got %d", pauseCount)
	}
	if totalPause < 20*time.Microsecond {
		t.Errorf("expected total pause >= 20µs, got %v", totalPause)
	}
}

func TestAdaptiveController_UpdateLevel(t *testing.T) {
	c := NewAdaptiveController(AdaptiveConfig{
		HighWatermark: 1000,
		LowWatermark:  500,
	})

	// Below low watermark - not paused
	c.UpdateLevel(100)
	if c.IsPaused() {
		t.Error("expected not paused below low watermark")
	}

	// Above high watermark - paused
	c.UpdateLevel(1500)
	if !c.IsPaused() {
		t.Error("expected paused above high watermark")
	}

	// Back to below low watermark - resumed and backoff reset
	c.UpdateLevel(100)
	if c.IsPaused() {
		t.Error("expected not paused below low watermark")
	}
}

func TestAdaptiveController_Concurrent(t *testing.T) {
	c := NewAdaptiveController(AdaptiveConfig{})
	var wg sync.WaitGroup

	// Concurrent operations
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			c.Pause()
		}()
		go func() {
			defer wg.Done()
			c.Resume()
		}()
		go func() {
			defer wg.Done()
			c.CheckAndYield()
		}()
	}

	wg.Wait()
	// Just verify no race conditions
}

func TestAdaptiveController_SetWatermarks(t *testing.T) {
	c := NewAdaptiveController(AdaptiveConfig{})
	c.SetWatermarks(1000, 2000)

	c.UpdateLevel(1500)
	if c.IsPaused() {
		t.Error("expected not paused at 1500 with high=2000")
	}

	c.UpdateLevel(2500)
	if !c.IsPaused() {
		t.Error("expected paused at 2500 with high=2000")
	}
}
