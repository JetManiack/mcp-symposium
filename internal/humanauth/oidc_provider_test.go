package humanauth_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"mcp-symposium/internal/humanauth"
	"mcp-symposium/internal/storage"
)

var testEncryptionKey = make([]byte, 32) // all-zero key is fine for tests, never used in production

type fakeRefresher struct {
	identity     humanauth.Identity
	refreshToken string
	err          error
}

func (f fakeRefresher) Refresh(refreshToken string) (humanauth.Identity, string, error) {
	return f.identity, f.refreshToken, f.err
}

func newSessionCookieRequest(sessionID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: humanauth.SessionCookieName, Value: sessionID})
	return req
}

func encryptForTesting(t *testing.T, plaintext string) string {
	t.Helper()
	encrypted, err := humanauth.EncryptRefreshTokenForTesting(testEncryptionKey, plaintext)
	if err != nil {
		t.Fatalf("EncryptRefreshTokenForTesting() error = %v", err)
	}
	return encrypted
}

func TestOIDCProvider_ReturnsCachedIdentityBeforeExpiry(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	session := &storage.Session{
		ID:           humanauth.HashSessionIDForTesting("raw-session-1"),
		Subject:      "subject-1",
		DisplayName:  "Jane Admin",
		Role:         "admin",
		RefreshToken: encryptForTesting(t, "original-refresh-token"),
		ExpiresAt:    time.Now().Add(15 * time.Minute),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("Create(session) error = %v", err)
	}

	refresher := fakeRefresher{err: errors.New("should not be called")}
	provider := humanauth.NewOIDCProvider(db, refresher, testEncryptionKey)

	identity, err := provider.Authenticate(newSessionCookieRequest("raw-session-1"))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if identity.Subject != "subject-1" || identity.Role != "admin" {
		t.Errorf("identity = %+v, want cached subject-1/admin", identity)
	}
}

func TestOIDCProvider_RefreshesExpiredSessionAndUpdatesRole(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	session := &storage.Session{
		ID:           humanauth.HashSessionIDForTesting("raw-session-1"),
		Subject:      "subject-1",
		DisplayName:  "Jane Admin",
		Role:         "admin",
		RefreshToken: encryptForTesting(t, "original-refresh-token"),
		ExpiresAt:    time.Now().Add(-time.Minute), // already past the refresh checkpoint
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("Create(session) error = %v", err)
	}

	refresher := fakeRefresher{
		identity:     humanauth.Identity{Subject: "subject-1", DisplayName: "Jane Admin", Role: "viewer"},
		refreshToken: "rotated-refresh-token",
	}
	provider := humanauth.NewOIDCProvider(db, refresher, testEncryptionKey)

	identity, err := provider.Authenticate(newSessionCookieRequest("raw-session-1"))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if identity.Role != "viewer" {
		t.Errorf("Role = %q, want %q (refreshed claims should win over the stale cached role)", identity.Role, "viewer")
	}

	var updated storage.Session
	if err := db.First(&updated, "id = ?", humanauth.HashSessionIDForTesting("raw-session-1")).Error; err != nil {
		t.Fatalf("First(session) error = %v", err)
	}
	decryptedRefreshToken, err := humanauth.DecryptRefreshTokenForTesting(testEncryptionKey, updated.RefreshToken)
	if err != nil {
		t.Fatalf("DecryptRefreshTokenForTesting() error = %v", err)
	}
	if decryptedRefreshToken != "rotated-refresh-token" {
		t.Errorf("decrypted stored RefreshToken = %q, want the rotated token", decryptedRefreshToken)
	}
	if !updated.ExpiresAt.After(time.Now()) {
		t.Error("ExpiresAt was not advanced past now after a successful refresh")
	}
}

func TestOIDCProvider_DeletesSessionWhenRefreshFails(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	session := &storage.Session{
		ID:           humanauth.HashSessionIDForTesting("raw-session-1"),
		Subject:      "subject-1",
		DisplayName:  "Jane Admin",
		Role:         "admin",
		RefreshToken: encryptForTesting(t, "revoked-refresh-token"),
		ExpiresAt:    time.Now().Add(-time.Minute),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("Create(session) error = %v", err)
	}

	refresher := fakeRefresher{err: errors.New("refresh token revoked")}
	provider := humanauth.NewOIDCProvider(db, refresher, testEncryptionKey)

	if _, err := provider.Authenticate(newSessionCookieRequest("raw-session-1")); err == nil {
		t.Fatal("Authenticate() error = nil, want an error when refresh fails")
	}

	var count int64
	db.Model(&storage.Session{}).Where("id = ?", humanauth.HashSessionIDForTesting("raw-session-1")).Count(&count)
	if count != 0 {
		t.Error("session row still exists after a failed refresh, want it deleted")
	}
}

func TestOIDCProvider_RejectsMissingCookie(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	provider := humanauth.NewOIDCProvider(db, fakeRefresher{}, testEncryptionKey)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := provider.Authenticate(req); err == nil {
		t.Fatal("Authenticate() error = nil, want an error with no session cookie")
	}
}

func TestOIDCProvider_RejectsUnknownSessionID(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	provider := humanauth.NewOIDCProvider(db, fakeRefresher{}, testEncryptionKey)

	if _, err := provider.Authenticate(newSessionCookieRequest("nonexistent-session")); err == nil {
		t.Fatal("Authenticate() error = nil, want an error for an unknown session ID")
	}
}

func TestOIDCProvider_NeverStoresRawSessionIDInDatabase(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	session := &storage.Session{
		ID:           humanauth.HashSessionIDForTesting("raw-session-1"),
		Subject:      "subject-1",
		DisplayName:  "Jane Admin",
		Role:         "admin",
		RefreshToken: encryptForTesting(t, "original-refresh-token"),
		ExpiresAt:    time.Now().Add(15 * time.Minute),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("Create(session) error = %v", err)
	}

	var rows []storage.Session
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("Find(sessions) error = %v", err)
	}
	for _, s := range rows {
		if s.ID == "raw-session-1" {
			t.Fatal("the raw session cookie value was stored directly as the primary key — a DB read/leak would let an attacker hijack any session by setting this value as their own cookie")
		}
	}
}
