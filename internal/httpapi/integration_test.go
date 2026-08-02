package httpapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ip2country/internal/config"
	geocsv "go-ip2country/internal/geo/csv"
	"go-ip2country/internal/httpapi"
	"go-ip2country/internal/ratelimit"
)

func TestEndToEnd_FindCountryAndRateLimit(t *testing.T) {
	locator, err := geocsv.New(config.Config{DatastoreCSVPath: "../../testdata/ip2country.csv"})
	require.NoError(t, err)

	perIPLimiter := ratelimit.NewFixedWindow(1, time.Second)
	defer perIPLimiter.Close()
	globalLimiter := ratelimit.NewFixedWindow(100, time.Second)
	defer globalLimiter.Close()

	router := httpapi.NewRouter(locator, globalLimiter, perIPLimiter)
	server := httptest.NewServer(router)
	defer server.Close()

	resp1, err := http.Get(server.URL + "/v1/find-country?ip=2.22.233.255")
	require.NoError(t, err)
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	assert.Equal(t, http.StatusOK, resp1.StatusCode)
	assert.Equal(t, `{"country":"GBR","city":"London"}`+"\n", string(body1))

	resp2, err := http.Get(server.URL + "/v1/find-country?ip=2.22.233.255")
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, http.StatusTooManyRequests, resp2.StatusCode, "second request within the same second should be rate limited")
}
