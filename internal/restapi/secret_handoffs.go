package restapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"mcp-symposium/internal/humanauth"

	"mcp-symposium/internal/storage"
)

// listSecretHandoffsHandler is the operator feed: metadata only (from, to,
// label, class, timestamps, fetch/foreign-attempt audit), never envelopes.
// This is how the operator sees every transfer after the fact without
// being in the loop.
func listSecretHandoffsHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := storage.ListSecretHandoffs(db, 100)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

// approveSecretHandoffHandler releases an admin-class handoff. Deliberately
// REST-and-human-only: an agent token cannot approve, whatever it claims.
func approveSecretHandoffHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		approver := "operator"
		if a, ok := humanauth.ActorFromContext(r.Context()); ok {
			approver = a.DisplayName
		}
		if err := storage.ApproveSecretHandoff(db, id, approver, time.Now()); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "approved"})
	}
}
