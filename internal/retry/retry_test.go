package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetrier_Success(t *testing.T) {
	r := NewRetrier(DefaultConfig())

	callCount := 0
	err := r.Do(context.Background(), func() error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestRetrier_RetrySuccess(t *testing.T) {
	config := Config{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}
	r := NewRetrier(config)

	callCount := 0
	err := r.Do(context.Background(), func() error {
		callCount++
		if callCount < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

func TestRetrier_MaxAttemptsExceeded(t *testing.T) {
	config := Config{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}
	r := NewRetrier(config)

	callCount := 0
	err := r.Do(context.Background(), func() error {
		callCount++
		return errors.New("persistent error")
	})

	if err == nil {
		t.Error("expected error, got nil")
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

func TestRetrier_ContextCancellation(t *testing.T) {
	config := Config{
		MaxAttempts:  0, // unlimited
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
	}
	r := NewRetrier(config)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	callCount := 0
	err := r.Do(ctx, func() error {
		callCount++
		return errors.New("temporary error")
	})

	if err != ErrContextCanceled {
		t.Errorf("expected ErrContextCanceled, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call before timeout, got %d", callCount)
	}
}

func TestRetrier_RetryableError(t *testing.T) {
	temporaryErr := errors.New("temporary error")
	permanentErr := errors.New("permanent error")

	config := Config{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		RetryableError: func(err error) bool {
			return err == temporaryErr
		},
	}
	r := NewRetrier(config)

	// Should retry temporary error
	callCount := 0
	err := r.Do(context.Background(), func() error {
		callCount++
		if callCount < 3 {
			return temporaryErr
		}
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}

	// Should not retry permanent error
	callCount = 0
	err = r.Do(context.Background(), func() error {
		callCount++
		return permanentErr
	})

	if err != permanentErr {
		t.Errorf("expected permanentErr, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call for permanent error, got %d", callCount)
	}
}

func TestRetrier_ExponentialBackoff(t *testing.T) {
	config := Config{
		MaxAttempts:  5,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
		Jitter:       0, // No jitter for predictable testing
	}
	r := NewRetrier(config)

	var delays []time.Duration
	start := time.Now()

	_ = r.DoWithCallback(context.Background(), func() error {
		return errors.New("error")
	}, func(a Attempt) {
		if a.Delay > 0 {
			delays = append(delays, time.Since(start))
			start = time.Now()
		}
	})

	// Check that delays are increasing (approximately)
	for i := 1; i < len(delays); i++ {
		// Allow 50% tolerance due to timing variations
		expectedMin := time.Duration(float64(delays[i-1]) * 1.5)
		if delays[i] < expectedMin {
			t.Errorf("delay %d (%v) should be >= delay %d * 1.5 (%v)", i, delays[i], i-1, expectedMin)
		}
	}
}

func TestRetrier_MaxDelay(t *testing.T) {
	config := Config{
		MaxAttempts:  10,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     200 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       0,
	}
	r := NewRetrier(config)

	var maxDelay time.Duration
	_ = r.DoWithCallback(context.Background(), func() error {
		return errors.New("error")
	}, func(a Attempt) {
		if a.Delay > maxDelay {
			maxDelay = a.Delay
		}
	})

	// Max delay should not exceed configured max (with small tolerance)
	if maxDelay > config.MaxDelay+10*time.Millisecond {
		t.Errorf("max delay %v exceeded configured max %v", maxDelay, config.MaxDelay)
	}
}

func TestRetrier_WithMaxAttempts(t *testing.T) {
	r := WithMaxAttempts(2)

	callCount := 0
	err := r.Do(context.Background(), func() error {
		callCount++
		return errors.New("error")
	})

	if err == nil {
		t.Error("expected error")
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}

func TestRetrier_DoWithResult(t *testing.T) {
	r := NewRetrier(DefaultConfig())

	callCount := 0
	result, err := r.DoWithResult(context.Background(), func() (interface{}, error) {
		callCount++
		return "success", nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != "success" {
		t.Errorf("expected 'success', got %v", result)
	}
}

func TestDo(t *testing.T) {
	callCount := 0
	err := Do(func() error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestDoWithContext(t *testing.T) {
	callCount := 0
	err := DoWithContext(context.Background(), func() error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

// BenchmarkRetrier_Do benchmarks the Do method.
func BenchmarkRetrier_Do(b *testing.B) {
	r := NewRetrier(DefaultConfig())
	fn := func() error { return nil }

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Do(context.Background(), fn)
	}
}

// BenchmarkRetrier_DoWithRetry benchmarks Do with retries.
func BenchmarkRetrier_DoWithRetry(b *testing.B) {
	config := Config{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   2.0,
	}
	r := NewRetrier(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		callCount := 0
		r.Do(context.Background(), func() error {
			callCount++
			if callCount < 2 {
				return errors.New("error")
			}
			return nil
		})
	}
}
