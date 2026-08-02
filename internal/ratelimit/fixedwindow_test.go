package ratelimit_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"go-ip2country/internal/ratelimit"
)

func TestFixedWindow_AllowsUpToLimitThenBlocks(t *testing.T) {
	f := ratelimit.NewFixedWindow(2, time.Second)
	defer f.Close()

	assert.True(t, f.Allow("a"))
	assert.True(t, f.Allow("a"))
	assert.False(t, f.Allow("a"), "third request in the same window should be blocked")
}

func TestFixedWindow_TracksKeysIndependently(t *testing.T) {
	f := ratelimit.NewFixedWindow(1, time.Second)
	defer f.Close()

	assert.True(t, f.Allow("a"))
	assert.True(t, f.Allow("b"), "a different key should have its own budget")
	assert.False(t, f.Allow("a"), "key a is already at its limit")
}
