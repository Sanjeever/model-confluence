package admin

import (
	"sync"
	"time"
)

type loginAttempt struct {
	count       int
	windowStart time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]loginAttempt)}
}

func (l *loginLimiter) allowed(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.attempts[ip]
	if now.Sub(attempt.windowStart) >= 15*time.Minute {
		delete(l.attempts, ip)
		return true
	}
	return attempt.count < 5
}

func (l *loginLimiter) failed(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.attempts[ip]
	if attempt.windowStart.IsZero() || now.Sub(attempt.windowStart) >= 15*time.Minute {
		attempt = loginAttempt{windowStart: now}
	}
	attempt.count++
	l.attempts[ip] = attempt
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}
