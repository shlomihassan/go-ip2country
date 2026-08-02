package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFixedWindow_ResetsAfterWindowRollsOver(t *testing.T) {
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }

	f := newFixedWindow(1, time.Second, clock)
	defer f.Close()

	assert.True(t, f.Allow("a"))
	assert.False(t, f.Allow("a"), "still within the same window")

	current = current.Add(time.Second)
	assert.True(t, f.Allow("a"), "new window should reset the budget")
}

func TestFixedWindow_SweepEvictsExpiredCounters(t *testing.T) {
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }

	f := newFixedWindow(1, time.Second, clock)
	defer f.Close()

	f.Allow("a")
	assert.Len(t, f.counters, 1)

	current = current.Add(3 * time.Second) // >= 2*window
	f.sweep(current)

	assert.Empty(t, f.counters, "expired counters should be evicted by sweep")
}
