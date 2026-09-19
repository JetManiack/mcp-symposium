package humanauth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"gorm.io/gorm"

	"mcp-symposium/internal/storage"
)

// SessionCookieName is the name of the cookie carrying the session ID.
const SessionCookieName = "rendezvous_session"

// sessionRefreshInterval is how long a session's cached claims are
// trusted before Authenticate re-validates them against Keycloak via the
// stored refresh token — see
// docs/superpowers/specs/2026-07-24-ai-rendezvous-point-oidc-design.md.
const sessionRefreshInterval = 15 * time.Minute

// tokenRefresher exchanges a refresh token for freshly-verified claims
// and the (possibly rotated) refresh token to store for next time. The
// real implementation (keycloakRefresher, added alongside the OIDC HTTP
// flow) talks to Keycloak's token endpoint and verifies the returned ID
// token; tests inject a fake satisfying this method signature to
// exercise Authenticate's session/expiry logic without a live Keycloak.
type tokenRefresher interface {
	Refresh(refreshToken string) (identity Identity, newRefreshToken string, err error)
}

// OIDCProvider authenticates requests via a server-side session cookie,
// re-validating the session's claims against Keycloak (via refresher)
// once sessionRefreshInterval has elapsed since the last check.
type OIDCProvider struct {
	db            *gorm.DB
	refresher     tokenRefresher
	encryptionKey []byte
}

// NewOIDCProvider constructs an OIDCProvider. Production code (added
// alongside the OIDC HTTP flow) passes a refresher backed by Keycloak;
// tests pass a fake to exercise session/expiry logic without a live
// Keycloak. encryptionKey (32 bytes, AES-256) is used to encrypt/decrypt
// storage.Session.RefreshToken at rest — a later task wires the real key
// in from a CLI flag; this constructor is not responsible for sourcing it.
func NewOIDCProvider(db *gorm.DB, refresher tokenRefresher, encryptionKey []byte) OIDCProvider {
	return OIDCProvider{db: db, refresher: refresher, encryptionKey: encryptionKey}
}

// hashSessionID mirrors actor_repo.go's hashToken: never store a raw
// session identifier — only its hash — so a database read/leak alone
// cannot be used to hijack a session. The raw value only ever lives in
// the browser's cookie and is never persisted.
func hashSessionID(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// HashSessionIDForTesting exposes hashSessionID for this package's own
// tests, which construct storage.Session rows directly (bypassing the
// real login flow) and need to store IDs the same way Authenticate looks
// them up.
func HashSessionIDForTesting(raw string) string {
	return hashSessionID(raw)
}

func (p OIDCProvider) Authenticate(r *http.Request) (*Identity, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil, errors.New("no session cookie")
	}

	var session storage.Session
	if err := p.db.First(&session, "id = ?", hashSessionID(cookie.Value)).Error; err != nil {
		return nil, errors.New("no such session")
	}

	if time.Now().Before(session.ExpiresAt) {
		return &Identity{
			Subject:     session.Subject,
			DisplayName: session.DisplayName,
			Role:        session.Role,
		}, nil
	}

	refreshToken, err := decryptRefreshToken(p.encryptionKey, session.RefreshToken)
	if err != nil {
		p.db.Delete(&session)
		return nil, fmt.Errorf("decrypt stored refresh token: %w", err)
	}

	identity, newRefreshToken, err := p.refresher.Refresh(refreshToken)
	if err != nil {
		p.db.Delete(&session)
		return nil, errors.New("session refresh failed: " + err.Error())
	}

	encryptedNewToken, err := encryptRefreshToken(p.encryptionKey, newRefreshToken)
	if err != nil {
		return nil, fmt.Errorf("encrypt refreshed token: %w", err)
	}

	session.Subject = identity.Subject
	session.DisplayName = identity.DisplayName
	session.Role = identity.Role
	session.RefreshToken = encryptedNewToken
	session.ExpiresAt = time.Now().Add(sessionRefreshInterval)
	if err := p.db.Save(&session).Error; err != nil {
		return nil, err
	}

	return &identity, nil
}
