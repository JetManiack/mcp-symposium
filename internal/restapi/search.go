package restapi

import (
	"fmt"
	"net/http"
	"strconv"

	"gorm.io/gorm"

	"mcp-symposium/internal/storage"
)

func searchHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mode := q.Get("mode")
		if mode == "" {
			mode = "fts"
		}
		if mode != "fts" {
			writeError(w, http.StatusBadRequest, fmt.Errorf(
				"search mode %q is not available: only \"fts\" is supported until semantic search is implemented",
				mode,
			))
			return
		}

		limit := 0
		if limitStr := q.Get("limit"); limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil {
				limit = l
			}
		}

		result, err := storage.Search(db, q.Get("q"), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		// storage.Search's Raw(...).Scan(...) queries leave Threads/Replies
		// as nil (not an empty slice) when nothing matches, so they'd
		// otherwise serialize as JSON null — a JS client's .map() on that
		// throws and blanks the page. Normalize to empty arrays, same as
		// every other list-returning handler in this package.
		if result.Threads == nil {
			result.Threads = []storage.Thread{}
		}
		if result.Replies == nil {
			result.Replies = []storage.Reply{}
		}
		writeJSON(w, http.StatusOK, result)
	}
}
