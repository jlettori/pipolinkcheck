package main

import (
	"sync"
	"time"
)

// RateLimiter limits the rate of requests to a fixed number per second,
// enforced globally across all callers. The rate can be adjusted at runtime via
// Increase or Decrease.
type RateLimiter struct {
	mu          sync.Mutex // guards rate, initialRate, nextAllowed, and stopped.
	rate        int        // current maximum requests per second.
	initialRate int        // ceiling the rate recovers to after a Decrease.
	nextAllowed time.Time  // earliest time the next request may fire.
	stopCh      chan struct{}
	stopped     bool
}

// NewRateLimiter creates a RateLimiter that allows up to n requests per second.
// The rate never exceeds the configured n (it recovers up to n after a
// Decrease); a background goroutine increases the rate by 1 every second toward
// that ceiling.
func NewRateLimiter(requestsPerSecond int) *RateLimiter {
	requestsPerSecond = clampReqsPerSecond(requestsPerSecond)

	rl := &RateLimiter{
		rate:        requestsPerSecond,
		initialRate: requestsPerSecond,
		stopCh:      make(chan struct{}),
	}
	go rl.autoIncrease()

	return rl
}

// clampReqsPerSecond bounds n to the supported requests-per-second range.
func clampReqsPerSecond(n int) int {
	return max(minReqsPerSecond, min(maxReqsPerSecond, n))
}

func (rl *RateLimiter) autoIncrease() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.Increase()
		case <-rl.stopCh:
			return
		}
	}
}

// Increase adds 1 to the maximum requests per second, up to the configured rate.
func (rl *RateLimiter) Increase() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.stopped {
		return
	}

	rl.rate = min(rl.initialRate, rl.rate+1)
}

// Decrease subtracts 1 from the maximum requests per second (minimum 1).
func (rl *RateLimiter) Decrease() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.stopped {
		return
	}

	rl.rate = max(minReqsPerSecond, rl.rate-1)
}

// Wait blocks until the next request is allowed by the rate limit. Each call
// reserves the next globally spaced slot, so concurrent callers are throttled
// to the configured rate as a whole: a caller that arrives while others are
// waiting joins the queue instead of sleeping a full interval on its own.
func (rl *RateLimiter) Wait() {
	rl.mu.Lock()
	if rl.stopped {
		rl.mu.Unlock()
		return
	}
	interval := time.Second / time.Duration(rl.rate)
	now := time.Now()
	if rl.nextAllowed.Before(now) {
		rl.nextAllowed = now
	}
	wait := rl.nextAllowed.Sub(now)
	rl.nextAllowed = rl.nextAllowed.Add(interval)
	rl.mu.Unlock()

	if wait > 0 {
		time.Sleep(wait)
	}
}

// Stop stops the auto-increase goroutine and marks the limiter as stopped.
func (rl *RateLimiter) Stop() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if !rl.stopped {
		rl.stopped = true
		close(rl.stopCh)
	}
}
