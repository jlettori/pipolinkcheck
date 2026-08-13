package main

import (
	"sync"
	"time"
)

// RateLimiter controls the rate of requests using a minimum interval between
// consecutive requests. The rate can be adjusted at runtime via Increase or
// Decrease.
type RateLimiter struct {
	mu          sync.Mutex    // mu guards rate, initialRate, and stopped.
	rate        int           // rate is the current maximum requests per second.
	initialRate int           // initialRate is the ceiling the rate recovers to after a Decrease.
	stopCh      chan struct{} // stopCh closes to stop the auto-increase goroutine.
	stopped     bool          // stopped is true once Stop has been called.
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

// Wait blocks until the next request is allowed by the rate limit.
func (rl *RateLimiter) Wait() {
	// Build a fresh timer for the current rate under the lock. A per-call
	// timer avoids sharing a single time.Ticker between Wait and the resizing
	// methods, which would race on Ticker.Reset (and could deadlock Wait after
	// Stop, as stopping a ticker never drains its channel).
	rl.mu.Lock()
	interval := time.Second / time.Duration(rl.rate)
	rl.mu.Unlock()

	timer := time.NewTimer(interval)
	defer timer.Stop()
	<-timer.C
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
