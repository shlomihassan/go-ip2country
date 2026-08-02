package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestNewRouter_HealthCheckNotRateLimited(t *testing.T) {
	router := httpapi.NewRouter(fakeLocator{}, fakeLimiter{allow: false}, fakeLimiter{allow: false})

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthReq.RemoteAddr = "203.0.113.1:12345"
	healthRec := httptest.NewRecorder()
	router.ServeHTTP(healthRec, healthReq)

	assert.Equal(t, http.StatusOK, healthRec.Code)

	findCountryReq := httptest.NewRequest(http.MethodGet, "/v1/find-country?ip=8.8.8.8", nil)
	findCountryReq.RemoteAddr = "203.0.113.1:12345"
	findCountryRec := httptest.NewRecorder()
	router.ServeHTTP(findCountryRec, findCountryReq)

	assert.Equal(t, http.StatusTooManyRequests, findCountryRec.Code)
}

func TestNewRouter_UnknownRouteReturnsJSON404(t *testing.T) {
	router := httpapi.NewRouter(fakeLocator{}, fakeLimiter{allow: true}, fakeLimiter{allow: true})

	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.NotEmpty(t, body["error"])
}

func TestNewRouter_WrongMethodReturnsJSON405(t *testing.T) {
	router := httpapi.NewRouter(fakeLocator{}, fakeLimiter{allow: true}, fakeLimiter{allow: true})

	req := httptest.NewRequest(http.MethodPost, "/v1/find-country", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.NotEmpty(t, body["error"])
}
