package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/example/disaster-response/internal/auth"
	"github.com/example/disaster-response/internal/ws"
)

// NewRouter wires routes and middleware.
func NewRouter(h *Handler, hub *ws.Hub, log *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Logger, chimw.Recoverer, auth.Middleware)

	r.Get("/healthz", h.Health)
	r.Get("/ws", hub.ServeWS) // outside the timeout group: long-lived connection

	r.Route("/disasters", func(r chi.Router) {
		r.Use(chimw.Timeout(15 * time.Second))
		r.With(auth.Require(auth.ActionRead)).Get("/", h.ListDisasters)
		r.With(auth.Require(auth.ActionCreate)).Post("/", h.CreateDisaster)
		r.Route("/{id}", func(r chi.Router) {
			r.With(auth.Require(auth.ActionRead)).Get("/", h.GetDisaster)
			r.With(auth.Require(auth.ActionUpdate)).Patch("/", h.UpdateDisaster)
			r.With(auth.Require(auth.ActionDelete)).Delete("/", h.DeleteDisaster)
			r.With(auth.Require(auth.ActionRead)).Get("/resources", h.NearbyResources)
			r.With(auth.Require(auth.ActionRead)).Get("/reports", h.DisasterReports)
		})
	})
	return r
}
