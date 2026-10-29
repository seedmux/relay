package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// connLimiter caps concurrent WebSocket connections per client IP, so one host
// cannot exhaust the relay by opening connections and never authenticating.
type connLimiter struct {
	mu    sync.Mutex
	max   int
	count map[string]int
}

func newConnLimiter(max int) *connLimiter {
	return &connLimiter{max: max, count: map[string]int{}}
}

func (l *connLimiter) acquire(ip string) bool {
	if l == nil || l.max <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.count[ip] >= l.max {
		return false
	}
	l.count[ip]++
	return true
}

func (l *connLimiter) release(ip string) {
	if l == nil || l.max <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.count[ip] <= 1 {
		delete(l.count, ip)
		return
	}
	l.count[ip]--
}

// clientIP is the address the limit applies to. Behind a reverse proxy on the
// same machine (the usual TLS setup), the proxy's X-Forwarded-For is used; it is
// ignored from any other peer, which could forge it.
func clientIP(r *http.Request, trustProxy bool) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if trustProxy {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
				// The proxy appends the address it saw last
				parts := strings.Split(forwarded, ",")
				if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
					return last
				}
			}
		}
	}
	return host
}

// rateLimiter is a token bucket per key (refill rate per second, burst capacity).
type rateLimiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(perMinute, burst int) *rateLimiter {
	return &rateLimiter{rate: float64(perMinute) / 60, burst: float64(burst), buckets: map[string]*bucket{}}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
