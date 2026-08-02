package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"go-ip2country/internal/geo"
	"go-ip2country/internal/ratelimit"
)

func NewRouter(locator geo.Locator, globalLimiter, perIPLimiter ratelimit.Limiter) http.Handler {
	r := chi.NewRouter()
	r.Use(RateLimit(globalLimiter, GlobalKey))
	r.Use(RateLimit(perIPLimiter, PerIPKey))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/v1/find-country", FindCountry(locator))

	return r
}
