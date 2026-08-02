package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"go-ip2country/internal/httpapi"
)

type fakeLimiter struct{ allow bool }

func (f fakeLimiter) Allow(string) bool { return f.allow }

func TestRateLimit_AllowsWhenLimiterAllows(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw := httpapi.RateLimit(fakeLimiter{allow: true}, httpapi.GlobalKey)
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRateLimit_BlocksWhenLimiterDenies(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	mw := httpapi.RateLimit(fakeLimiter{allow: false}, httpapi.GlobalKey)
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.False(t, called)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, `{"error":"rate limit exceeded"}`+"\n", rec.Body.String())
}
