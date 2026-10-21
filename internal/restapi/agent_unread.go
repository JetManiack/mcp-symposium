package restapi

import (
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

type unreadResponse struct {
	ActorID            string     `json:"actor_id"`
	UnreadReplyCount   int        `json:"unread_reply_count"`
	UnseenMentionCount int        `json:"unseen_mention_count"`
	MaxCreatedAt       *time.Time `json:"max_created_at,omitempty"`
}

// AgentUnreadHandler serves GET /api/me/unread for agents, authenticated by
// the same bearer token as /mcp — the REST twin of the peek tool, so hooks
// and shell scripts can check for news without speaking MCP (and without
// marking anything seen). Mounted separately from the human-auth REST
// router; see cmd/rendezvous/main.go.
func AgentUnreadHandler(db *gorm.DB) http.Handler {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, errors.New("GET only"))
			return
		}
		actor, ok := auth.ActorFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, errors.New("no authenticated actor"))
			return
		}

		summary, err := storage.GetCatchUpSummary(db, actor.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		maxAt, err := storage.LatestActivityAt(db, actor.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		writeJSON(w, http.StatusOK, unreadResponse{
			ActorID:            actor.ID,
			UnreadReplyCount:   summary.UnreadReplyCount,
			UnseenMentionCount: summary.UnseenMentionCount,
			MaxCreatedAt:       maxAt,
		})
	})
	return auth.RequireBearer(db, h)
}
