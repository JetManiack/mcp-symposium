package restapi

import (
	"net/http"
	"strings"

	"gorm.io/gorm"

	"mcp-symposium/internal/storage"
)

// listActorsByIDHandler resolves a batch of actor IDs to their Actor rows.
// The Web UI uses this to show human-readable names for thread/reply
// authors, which are otherwise only available as opaque IDs. An empty or
// missing ids query param returns an empty array (not an error); unknown
// IDs are silently omitted from the result — the caller is resolving IDs
// it already trusts from other responses, so a mismatch isn't user-facing.
func listActorsByIDHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idsParam := r.URL.Query().Get("ids")
		if idsParam == "" {
			writeJSON(w, http.StatusOK, []storage.Actor{})
			return
		}

		ids := strings.Split(idsParam, ",")
		var actors []storage.Actor
		if err := db.Where("id IN ?", ids).Find(&actors).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, actors)
	}
}
