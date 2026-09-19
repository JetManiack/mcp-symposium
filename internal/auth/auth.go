package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"mcp-symposium/internal/storage"
)

type contextKey string

const actorContextKey contextKey = "actor"

func withActor(ctx context.Context, actor *storage.Actor) context.Context {
	return context.WithValue(ctx, actorContextKey, actor)
}

// ActorFromContext returns the Actor authenticated by RequireBearer for
// the current request, if any.
func ActorFromContext(ctx context.Context) (*storage.Actor, bool) {
	actor, ok := ctx.Value(actorContextKey).(*storage.Actor)
	return actor, ok
}

// RequireBearer authenticates every request by its Authorization: Bearer
// header and injects the resulting Actor into the request context for
// downstream handlers to read via ActorFromContext.
func RequireBearer(db *gorm.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="rendezvous"`)
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}

		actor, err := Authenticate(db, token)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="rendezvous"`)
			http.Error(w, "invalid or revoked token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r.WithContext(withActor(r.Context(), actor)))
	})
}

// Authenticate looks up a raw bearer token, updates its last-used
// timestamp, and returns the owning Actor.
func Authenticate(db *gorm.DB, raw string) (*storage.Actor, error) {
	var cred storage.AgentCredential
	err := db.Where("token_hash = ? AND revoked_at IS NULL", hashToken(raw)).First(&cred).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, storage.ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}

	now := time.Now()
	if err := db.Model(&cred).Update("last_used_at", now).Error; err != nil {
		slog.Warn("failed to update agent credential last_used_at", "credential_id", cred.ID, "error", err)
	}

	var actor storage.Actor
	if err := db.First(&actor, "id = ?", cred.ActorID).Error; err != nil {
		return nil, err
	}
	return &actor, nil
}

// Issue generates a new bearer token for actorID and persists only its
// hash. The raw token is returned once and never stored.
func Issue(db *gorm.DB, actorID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	rawToken := "arp_" + base64.RawURLEncoding.EncodeToString(raw)

	cred := &storage.AgentCredential{
		ID:        uuid.NewString(),
		ActorID:   actorID,
		TokenHash: hashToken(rawToken),
	}
	if err := db.Create(cred).Error; err != nil {
		return "", err
	}
	return rawToken, nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
