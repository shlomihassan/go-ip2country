package httpapi_test

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"go-ip2country/internal/geo"
	"go-ip2country/internal/httpapi"
)

type fakeLocator struct {
	loc geo.Location
	err error
}

func (f fakeLocator) Lookup(net.IP) (geo.Location, error) {
	return f.loc, f.err
}

func TestFindCountry(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		locator    fakeLocator
		wantStatus int
		wantBody   string
	}{
		{
			name:       "success",
			url:        "/v1/find-country?ip=8.8.8.8",
			locator:    fakeLocator{loc: geo.Location{Country: "USA", City: "Mountain View"}},
			wantStatus: http.StatusOK,
			wantBody:   `{"country":"USA","city":"Mountain View"}` + "\n",
		},
		{
			name:       "missing ip param",
			url:        "/v1/find-country",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"missing required query parameter: ip"}` + "\n",
		},
		{
			name:       "invalid ip",
			url:        "/v1/find-country?ip=not-an-ip",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid ip address"}` + "\n",
		},
		{
			name:       "not found",
			url:        "/v1/find-country?ip=9.9.9.9",
			locator:    fakeLocator{err: geo.ErrNotFound},
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":"location not found for ip"}` + "\n",
		},
		{
			name:       "internal error",
			url:        "/v1/find-country?ip=9.9.9.9",
			locator:    fakeLocator{err: errors.New("boom")},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"internal server error"}` + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			rec := httptest.NewRecorder()

			httpapi.FindCountry(tt.locator)(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantBody, rec.Body.String())
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		})
	}
}
