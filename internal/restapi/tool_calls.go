package restapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"mcp-symposium/internal/storage"
)

// listToolCallsHandler handles GET /api/tool-calls.
// Query params:
//   - actor=<actor_id>  — filter by actor
//   - tool=<name>       — filter by tool name
//   - limit=<n>         — max rows to return (default 50, max 500)
//   - before=<ISO 8601> — only calls before this timestamp
func listToolCallsHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		query := db.Model(&storage.ToolCall{}).Order("called_at DESC")

		if actor := q.Get("actor"); actor != "" {
			query = query.Where("actor_id = ?", actor)
		}
		if tool := q.Get("tool"); tool != "" {
			query = query.Where("tool = ?", tool)
		}
		if before := q.Get("before"); before != "" {
			t, err := time.Parse(time.RFC3339, before)
			if err != nil {
				writeError(w, http.StatusBadRequest, errors.New("before must be an ISO 8601 timestamp"))
				return
			}
			query = query.Where("called_at < ?", t)
		}

		limit := 50
		if s := q.Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				if n > 500 {
					n = 500
				}
				limit = n
			}
		}
		query = query.Limit(limit)

		var calls []storage.ToolCall
		if err := query.Find(&calls).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if calls == nil {
			calls = []storage.ToolCall{}
		}
		writeJSON(w, http.StatusOK, calls)
	}
}

// getToolCallHandler handles GET /api/tool-calls/{id}.
func getToolCallHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var call storage.ToolCall
		if err := db.First(&call, "id = ?", id).Error; err != nil {
			writeError(w, http.StatusNotFound, errors.New("tool call not found"))
			return
		}
		writeJSON(w, http.StatusOK, call)
	}
}
