package main

import (
	"sync"
	"testing"
	"time"
)

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10)
	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}
	rl.Stop()
}

func TestNewRateLimiter_DefaultsToOne(t *testing.T) {
	t.Run("zero", func(t *testing.T) {
		rl := NewRateLimiter(0)
		if rl == nil {
			t.Fatal("NewRateLimiter(0) returned nil")
		}
		defer rl.Stop()
	})

	t.Run("negative", func(t *testing.T) {
		rl := NewRateLimiter(-5)
		if rl == nil {
			t.Fatal("NewRateLimiter(-5) returned nil")
		}
		defer rl.Stop()
	})
}

func TestRateLimiter_WaitBlocks(t *testing.T) {
	rl := NewRateLimiter(100)
	defer rl.Stop()

	start := time.Now()
	rl.Wait()
	elapsed := time.Since(start)

	if elapsed < 0 {
		t.Errorf("Wait completed in negative time: %v", elapsed)
	}
}

func TestRateLimiter_WaitThrottles(t *testing.T) {
	rl := NewRateLimiter(10)
	defer rl.Stop()

	start := time.Now()
	for i := 0; i < 10; i++ {
		rl.Wait()
	}
	elapsed := time.Since(start)

	minExpected := 900 * time.Millisecond
	if elapsed < minExpected {
		t.Errorf("10 waits at 10/s took %v; expected at least %v", elapsed, minExpected)
	}
}

func TestRateLimiter_WaitPacesAcrossSecond(t *testing.T) {
	rl := NewRateLimiter(5)
	defer rl.Stop()

	start := time.Now()
	for i := 0; i < 5; i++ {
		rl.Wait()
	}
	elapsed := time.Since(start)

	if elapsed < 800*time.Millisecond {
		t.Errorf("5 waits at 5/s took %v; expected ~1s", elapsed)
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(100)
	defer rl.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rl.Wait()
		}()
	}
	wg.Wait()
}

func TestRateLimiter_GlobalRateAcrossConcurrentCallers(t *testing.T) {
	rl := NewRateLimiter(10)
	defer rl.Stop()

	// Two callers issuing 5 waits each: if the rate were enforced per caller,
	// both would finish in ~500ms. Enforced globally, the 10 requests must be
	// spaced at 10/s (~900ms total).
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				rl.Wait()
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	if elapsed < 800*time.Millisecond {
		t.Errorf("10 concurrent waits at 10/s took %v; want >= 800ms (global rate)", elapsed)
	}
}

func TestRateLimiter_StopIdempotent(t *testing.T) {
	rl := NewRateLimiter(10)
	rl.Stop()
	rl.Stop()
}

func TestRateLimiter_IncreaseCapsAtConfiguredRate(t *testing.T) {
	rl := NewRateLimiter(minReqsPerSecond)
	defer rl.Stop()

	rl.Increase()
	if rl.rate != minReqsPerSecond {
		t.Errorf("Increase beyond configured rate: rate = %d; want %d", rl.rate, minReqsPerSecond)
	}
}

func TestRateLimiter_IncreaseRecoversTowardConfiguredRate(t *testing.T) {
	rl := NewRateLimiter(5)
	defer rl.Stop()

	rl.Decrease()
	if rl.rate != 4 {
		t.Fatalf("Decrease: rate = %d; want 4", rl.rate)
	}
	rl.Increase()
	if rl.rate != 5 {
		t.Errorf("Increase after Decrease: rate = %d; want 5", rl.rate)
	}
	rl.Increase()
	if rl.rate != 5 {
		t.Errorf("Increase beyond configured rate: rate = %d; want 5", rl.rate)
	}
}

func TestRateLimiter_IncreaseCapsAtMax(t *testing.T) {
	rl := NewRateLimiter(maxReqsPerSecond)
	defer rl.Stop()

	rl.Increase()
	if rl.rate != maxReqsPerSecond {
		t.Errorf("Increase beyond max: rate = %d; want %d", rl.rate, maxReqsPerSecond)
	}
}

func TestRateLimiter_IncreaseAfterStop(t *testing.T) {
	rl := NewRateLimiter(10)
	rl.Stop()
	before := rl.rate
	rl.Increase()
	if rl.rate != before {
		t.Errorf("Increase after Stop changed rate: %d -> %d", before, rl.rate)
	}
}

func TestRateLimiter_Decrease(t *testing.T) {
	rl := NewRateLimiter(maxReqsPerSecond)
	defer rl.Stop()

	before := rl.rate
	rl.Decrease()
	after := rl.rate

	if after != before-1 {
		t.Errorf("Decrease: rate %d -> %d; want -1", before, after)
	}
}

func TestRateLimiter_DecreaseFloorsAtMin(t *testing.T) {
	rl := NewRateLimiter(minReqsPerSecond)
	defer rl.Stop()

	rl.Decrease()
	if rl.rate != minReqsPerSecond {
		t.Errorf("Decrease below min: rate = %d; want %d", rl.rate, minReqsPerSecond)
	}
}

func TestRateLimiter_DecreaseAfterStop(t *testing.T) {
	rl := NewRateLimiter(10)
	rl.Stop()
	before := rl.rate
	rl.Decrease()
	if rl.rate != before {
		t.Errorf("Decrease after Stop changed rate: %d -> %d", before, rl.rate)
	}
}
