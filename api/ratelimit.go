package main

import (
	"sync"
	"time"
)

// limiter is a per-key token bucket held in memory. Keys are client IPs.
// Memory is bounded by periodic sweeps of idle buckets.
type limiter struct {
	mu     sync.Mutex
	rate   float64 // tokens per second
	burst  float64
	bucket map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(perMinute, burst int) *limiter {
	l := &limiter{rate: float64(perMinute) / 60, burst: float64(burst), bucket: map[string]*bucket{}}
	go func() {
		for range time.Tick(5 * time.Minute) {
			l.sweep()
		}
	}()
	return l
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := time.Now()
	b, ok := l.bucket[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: n}
		l.bucket[key] = b
	}
	b.tokens += n.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = n
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *limiter) sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-10 * time.Minute)
	for k, b := range l.bucket {
		if b.last.Before(cut) {
			delete(l.bucket, k)
		}
	}
}
