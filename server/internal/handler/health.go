package handler

import (
	"net/http"
	"time"
)

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Ping(); err != nil {
		h.log.Debug("health store ping", "err", err)
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":  "error",
			"version": h.version,
			"time":    time.Now().UTC(),
			"store":   map[string]any{"ok": false, "error": "store unavailable"},
		})
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": h.version,
		"time":    time.Now().UTC(),
		"store":   map[string]any{"ok": true},
	})
}
