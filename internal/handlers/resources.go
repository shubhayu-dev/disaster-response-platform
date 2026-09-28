package handlers

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"github.com/example/disaster-response/internal/models"
)

const (
	defaultRadiusKm = 10.0
	maxRadiusKm     = 1000.0
)

// NearbyResources handles GET /disasters/{id}/resources?lat=&lng=&radius=.
// lat/lng default to the disaster's own coordinates; radius is in kilometres.
func (h *Handler) NearbyResources(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	lat, err := floatParam(q, "lat")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	lng, err := floatParam(q, "lng")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	radius, err := floatParam(q, "radius")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if (lat == nil) != (lng == nil) {
		writeError(w, http.StatusBadRequest, "lat and lng must be provided together")
		return
	}
	radiusKm := defaultRadiusKm
	if radius != nil {
		if *radius <= 0 || *radius > maxRadiusKm {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("radius must be in (0, %.0f] km", maxRadiusKm))
			return
		}
		radiusKm = *radius
	}

	d, err := h.store.GetDisaster(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	cLat, cLng := d.Latitude, d.Longitude
	if lat != nil {
		if !models.ValidCoordinate(*lat, *lng) {
			writeError(w, http.StatusBadRequest, "lat must be in [-90, 90] and lng in [-180, 180]")
			return
		}
		cLat, cLng = *lat, *lng
	}

	resources, err := h.store.NearbyResources(r.Context(), id, cLat, cLng, radiusKm)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"disaster_id": id,
		"center":      map[string]float64{"latitude": cLat, "longitude": cLng},
		"radius_km":   radiusKm,
		"count":       len(resources),
		"resources":   resources,
	})
}

func floatParam(q url.Values, name string) (*float64, error) {
	raw := q.Get(name)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, fmt.Errorf("%s must be a finite number", name)
	}
	return &v, nil
}
