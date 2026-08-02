package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"go-ip2country/internal/geo"
	"go-ip2country/internal/ratelimit"
)

func NewRouter(locator geo.Locator, globalLimiter, perIPLimiter ratelimit.Limiter) http.Handler {
	r := chi.NewRouter()

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	})

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.Group(func(r chi.Router) {
		r.Use(RateLimit(globalLimiter, GlobalKey))
		r.Use(RateLimit(perIPLimiter, PerIPKey))
		r.Get("/v1/find-country", FindCountry(locator))
	})

	return r
}
