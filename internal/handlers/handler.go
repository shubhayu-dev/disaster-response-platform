// Package handlers contains the HTTP layer.
package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

	"github.com/example/disaster-response/internal/geo"
	"github.com/example/disaster-response/internal/models"
	"github.com/example/disaster-response/internal/reports"
	"github.com/example/disaster-response/internal/store"
)

// Store is the persistence contract the handlers depend on.
type Store interface {
	Ping(ctx context.Context) error
	CreateDisaster(ctx context.Context, d models.Disaster) (models.Disaster, error)
	ListDisasters(ctx context.Context, f store.ListFilter) ([]models.Disaster, error)
	GetDisaster(ctx context.Context, id string) (models.Disaster, error)
	UpdateDisaster(ctx context.Context, id string, req models.UpdateDisasterRequest) (models.Disaster, error)
	DeleteDisaster(ctx context.Context, id string) error
	NearbyResources(ctx context.Context, disasterID string, lat, lng, radiusKm float64) ([]models.Resource, error)
}

// Broadcaster pushes real-time events to WebSocket clients.
type Broadcaster interface {
	Broadcast(event string, data any)
}

// LocationResolver resolves coordinates from text or names.
type LocationResolver interface {
	Resolve(ctx context.Context, description string) (geo.Location, error)
	Lookup(name string) (geo.Location, bool)
}

// ReportService provides cache-aside access to external reports.
type ReportService interface {
	Get(ctx context.Context, d models.Disaster) ([]reports.Report, bool, error)
	Invalidate(ctx context.Context, disasterID string)
}

// Handler bundles dependencies for all routes.
type Handler struct {
	store    Store
	hub      Broadcaster
	resolver LocationResolver
	reports  ReportService
	log      *slog.Logger
}

// New constructs a Handler.
func New(s Store, hub Broadcaster, r LocationResolver, rs ReportService, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{store: s, hub: hub, resolver: r, reports: rs, log: log}
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// idParam extracts and validates the {id} URL parameter.
func idParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "id")
	if !uuidRe.MatchString(id) {
		writeError(w, http.StatusBadRequest, "id must be a valid UUID")
		return "", false
	}
	return id, true
}

// fail maps store errors onto HTTP responses.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "disaster not found")
		return
	}
	h.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

// Health reports liveness and database connectivity.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
