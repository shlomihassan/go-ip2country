package ratelimit

import (
	"sync"
	"time"
)

type Limiter interface {
	Allow(key string) bool
}

type windowCounter struct {
	count       int
	windowStart time.Time
}

type FixedWindow struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	counters map[string]*windowCounter
	now      func() time.Time
	stop     chan struct{}
}

func NewFixedWindow(limit int, window time.Duration) *FixedWindow {
	return newFixedWindow(limit, window, time.Now)
}

func newFixedWindow(limit int, window time.Duration, now func() time.Time) *FixedWindow {
	f := &FixedWindow{
		limit:    limit,
		window:   window,
		counters: make(map[string]*windowCounter),
		now:      now,
		stop:     make(chan struct{}),
	}
	go f.sweepLoop()
	return f
}

func (f *FixedWindow) Allow(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := f.now()
	c, ok := f.counters[key]
	if !ok || now.Sub(c.windowStart) >= f.window {
		c = &windowCounter{count: 0, windowStart: now}
		f.counters[key] = c
	}

	if c.count >= f.limit {
		return false
	}
	c.count++
	return true
}

func (f *FixedWindow) sweepLoop() {
	ticker := time.NewTicker(f.window * 10)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			f.sweep(f.now())
		case <-f.stop:
			return
		}
	}
}

func (f *FixedWindow) sweep(now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, c := range f.counters {
		if now.Sub(c.windowStart) >= 2*f.window {
			delete(f.counters, key)
		}
	}
}

func (f *FixedWindow) Close() {
	close(f.stop)
}
