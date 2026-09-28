package handlers

import "net/http"

// DisasterReports handles GET /disasters/{id}/reports (Redis cache-aside).
func (h *Handler) DisasterReports(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	d, err := h.store.GetDisaster(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items, cached, err := h.reports.Get(r.Context(), d)
	if err != nil {
		h.log.Error("external reports unavailable", "disaster_id", id, "err", err)
		writeError(w, http.StatusBadGateway, "external report source unavailable")
		return
	}
	if cached {
		w.Header().Set("X-Cache", "HIT")
	} else {
		w.Header().Set("X-Cache", "MISS")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"disaster_id": id,
		"cached":      cached,
		"count":       len(items),
		"reports":     items,
	})
}
