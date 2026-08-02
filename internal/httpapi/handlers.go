package httpapi

import (
	"errors"
	"net"
	"net/http"

	"go-ip2country/internal/geo"
)

type findCountryResponse struct {
	Country string `json:"country"`
	City    string `json:"city"`
}

func FindCountry(locator geo.Locator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ipParam := r.URL.Query().Get("ip")
		if ipParam == "" {
			writeError(w, http.StatusBadRequest, "missing required query parameter: ip")
			return
		}

		ip := net.ParseIP(ipParam)
		if ip == nil {
			writeError(w, http.StatusBadRequest, "invalid ip address")
			return
		}

		loc, err := locator.Lookup(ip)
		if errors.Is(err, geo.ErrNotFound) {
			writeError(w, http.StatusNotFound, "location not found for ip")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, findCountryResponse{Country: loc.Country, City: loc.City})
	}
}
