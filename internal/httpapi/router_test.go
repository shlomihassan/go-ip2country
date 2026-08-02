package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"go-ip2country/internal/geo"
	"go-ip2country/internal/httpapi"
)

func TestNewRouter_HealthCheck(t *testing.T) {
	router := httpapi.NewRouter(fakeLocator{}, fakeLimiter{allow: true}, fakeLimiter{allow: true})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewRouter_FindCountryRoute(t *testing.T) {
	router := httpapi.NewRouter(
		fakeLocator{loc: geo.Location{Country: "USA", City: "Mountain View"}},
		fakeLimiter{allow: true},
		fakeLimiter{allow: true},
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/find-country?ip=8.8.8.8", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewRouter_GlobalRateLimitBlocks(t *testing.T) {
	router := httpapi.NewRouter(fakeLocator{}, fakeLimiter{allow: false}, fakeLimiter{allow: true})

	req := httptest.NewRequest(http.MethodGet, "/v1/find-country?ip=8.8.8.8", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}
