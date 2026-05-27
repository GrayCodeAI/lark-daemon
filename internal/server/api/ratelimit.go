package api

import (
	"sync"
	"time"
)

const staleThreshold = 10 * time.Minute

// RateLimiter provides simple per-IP rate limiting with automatic stale entry eviction.
type RateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientBucket
	limit   int
	stop    chan struct{}
}

type clientBucket struct {
	tokens    int
	lastCheck time.Time
}

func NewRateLimiter(limit int) *RateLimiter {
	rl := &RateLimiter{
		clients: make(map[string]*clientBucket),
		limit:   limit,
		stop:    make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// cleanup evicts stale IP entries every 5 minutes to prevent memory leaks.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, b := range rl.clients {
				if now.Sub(b.lastCheck) > staleThreshold {
					delete(rl.clients, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.stop:
			return
		}
	}
}

// Stop halts the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stop)
}

// Allow checks whether the given IP has tokens remaining.
// Returns true if the request is allowed, false if rate-limited.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.clients[ip]
	if !ok {
		b = &clientBucket{tokens: rl.limit, lastCheck: time.Now()}
		rl.clients[ip] = b
	}
	elapsed := time.Since(b.lastCheck).Seconds()
	b.lastCheck = time.Now()
	b.tokens += int(elapsed)
	if b.tokens > rl.limit {
		b.tokens = rl.limit
	}
	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}
