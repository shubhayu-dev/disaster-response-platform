package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/example/disaster-response/internal/auth"
	"github.com/example/disaster-response/internal/geo"
	"github.com/example/disaster-response/internal/models"
	"github.com/example/disaster-response/internal/store"
)

const (
	eventDisasterUpdated = "disaster_updated"
	defaultPageSize      = 50
	maxPageSize          = 200
)

// CreateDisaster handles POST /disasters.
func (h *Handler) CreateDisaster(w http.ResponseWriter, r *http.Request) {
	var req models.CreateDisasterRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if errs := req.Validate(); len(errs) > 0 {
		writeValidation(w, errs)
		return
	}
	name, lat, lng, err := h.resolveLocation(r.Context(), req)
	if err != nil {
		if errors.Is(err, geo.ErrLocationNotFound) {
			writeValidation(w, map[string]string{
				"location": "could not determine a location; mention a known place in the description or provide latitude and longitude",
			})
			return
		}
		h.fail(w, r, err)
		return
	}
	created, err := h.store.CreateDisaster(r.Context(), models.Disaster{
		Title: req.Title, Description: req.Description, LocationName: name,
		Latitude: lat, Longitude: lng, Tags: req.Tags, Status: req.Status,
		CreatedBy: auth.UserID(r.Context()),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.hub.Broadcast(eventDisasterUpdated, created)
	w.Header().Set("Location", "/disasters/"+created.ID)
	writeJSON(w, http.StatusCreated, created)
}

// resolveLocation precedence: explicit coordinates > explicit location_name
// (geocoded) > extraction from the description (LLM, then dictionary fallback).
func (h *Handler) resolveLocation(ctx context.Context, req models.CreateDisasterRequest) (string, float64, float64, error) {
	if req.Latitude != nil && req.Longitude != nil {
		name := req.LocationName
		if name == "" {
			name = fmt.Sprintf("%.4f, %.4f", *req.Latitude, *req.Longitude)
		}
		return name, *req.Latitude, *req.Longitude, nil
	}
	if req.LocationName != "" {
		loc, ok := h.resolver.Lookup(req.LocationName)
		if !ok {
			return "", 0, 0, geo.ErrLocationNotFound
		}
		return loc.Name, loc.Latitude, loc.Longitude, nil
	}
	loc, err := h.resolver.Resolve(ctx, req.Description)
	if err != nil {
		return "", 0, 0, err
	}
	return loc.Name, loc.Latitude, loc.Longitude, nil
}

// ListDisasters handles GET /disasters?tags=flood,fire&limit=&offset=.
func (h *Handler) ListDisasters(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var rawTags []string
	for _, v := range append(q["tags"], q["tag"]...) {
		rawTags = append(rawTags, strings.Split(v, ",")...)
	}
	limit, err := intParam(q.Get("limit"), defaultPageSize, 1, maxPageSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be an integer between 1 and %d", maxPageSize))
		return
	}
	offset, err := intParam(q.Get("offset"), 0, 0, 1<<30)
	if err != nil {
		writeError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return
	}
	items, err := h.store.ListDisasters(r.Context(), store.ListFilter{
		Tags: models.NormalizeTags(rawTags), Limit: limit, Offset: offset,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "count": len(items)})
}

// GetDisaster handles GET /disasters/{id}.
func (h *Handler) GetDisaster(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	d, err := h.store.GetDisaster(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// UpdateDisaster handles PATCH /disasters/{id}.
func (h *Handler) UpdateDisaster(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var req models.UpdateDisasterRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if errs := req.Validate(); len(errs) > 0 {
		writeValidation(w, errs)
		return
	}
	// A new location_name without coordinates is geocoded via the gazetteer.
	if req.LocationName != nil && req.Latitude == nil {
		loc, found := h.resolver.Lookup(*req.LocationName)
		if !found {
			writeValidation(w, map[string]string{
				"location_name": "unknown location; provide latitude and longitude as well",
			})
			return
		}
		req.LocationName = &loc.Name
		req.Latitude, req.Longitude = &loc.Latitude, &loc.Longitude
	}
	updated, err := h.store.UpdateDisaster(r.Context(), id, req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.reports.Invalidate(r.Context(), id)
	h.hub.Broadcast(eventDisasterUpdated, updated)
	writeJSON(w, http.StatusOK, updated)
}

// DeleteDisaster handles DELETE /disasters/{id}.
func (h *Handler) DeleteDisaster(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := h.store.DeleteDisaster(r.Context(), id); err != nil {
		h.fail(w, r, err)
		return
	}
	h.reports.Invalidate(r.Context(), id)
	w.WriteHeader(http.StatusNoContent)
}

func intParam(raw string, def, min, max int) (int, error) {
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < min || v > max {
		return 0, errors.New("out of range")
	}
	return v, nil
}
