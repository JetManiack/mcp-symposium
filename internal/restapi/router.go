package restapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"mcp-symposium/internal/humanauth"
)

// NewHandler builds the full REST API handler, mounted with no path
// prefix (the caller mounts it under /api — see cmd/rendezvous/main.go).
// Every route requires human authentication via provider.
func NewHandler(db *gorm.DB, provider humanauth.Provider) http.Handler {
	r := chi.NewRouter()
	r.Use(humanauth.RequireHumanAuth(db, provider))

	r.Route("/threads", func(r chi.Router) {
		r.Get("/", listThreadsHandler(db))
		r.With(humanauth.RequireAdmin).Post("/", createThreadHandler(db))
		r.Get("/{id}", getThreadHandler(db))
		r.With(humanauth.RequireAdmin).Post("/{id}/replies", addReplyHandler(db))
		r.With(humanauth.RequireAdmin).Patch("/{id}", updateThreadStatusHandler(db))
	})

	r.Get("/search", searchHandler(db))

	r.Get("/profiles/{actorID}", getProfileHandler(db))
	r.With(humanauth.RequireAdmin).Put("/profiles/{actorID}", updateProfileByIDHandler(db))
	r.Put("/me/profile", updateOwnProfileHandler(db))

	// Actors: reads for every role, management admin-only. One Route block:
	// chi sends both /actors and /actors/ into the subrouter, so a separate
	// top-level r.Get("/actors", ...) next to this Route is shadowed by the
	// subrouter's middleware — that combination 403'd viewers on plain reads.
	// GET keeps the pre-existing split the clients rely on: bare /actors
	// (ThreadDetail's ?ids= lookup) is readable by every role, while
	// /actors/ with the trailing slash is the credential-bearing agent
	// management listing (the admin UI's Agents page) and stays admin-only.
	r.Route("/actors", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			if strings.HasSuffix(req.URL.Path, "/") {
				humanauth.RequireAdmin(http.HandlerFunc(listAgentsHandler(db))).ServeHTTP(w, req)
				return
			}
			listActorsByIDHandler(db)(w, req)
		})
		r.With(humanauth.RequireAdmin).Post("/", createAgentHandler(db))
		r.With(humanauth.RequireAdmin).Delete("/{id}", deleteAgentHandler(db))
		r.With(humanauth.RequireAdmin).Post("/{id}/credentials", issueTokenHandler(db))
		r.With(humanauth.RequireAdmin).Put("/{id}/allowlist", setAllowlistHandler(db))
	})
	r.With(humanauth.RequireAdmin).Delete("/credentials/{id}", revokeTokenHandler(db))

	// Secret-handoff operator feed and admin-class approval (admin only;
	// metadata only — envelopes never leave their table through REST)
	r.Route("/secret-handoffs", func(r chi.Router) {
		r.Use(humanauth.RequireAdmin)
		r.Get("/", listSecretHandoffsHandler(db))
		r.Post("/{id}/approve", approveSecretHandoffHandler(db))
	})

	// Tool-call audit log (admin only)
	r.Route("/tool-calls", func(r chi.Router) {
		r.Use(humanauth.RequireAdmin)
		r.Get("/", listToolCallsHandler(db))
		r.Get("/{id}", getToolCallHandler(db))
	})

	r.Get("/me", meHandler())

	return r
}
