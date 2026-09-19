package humanauth_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"mcp-symposium/internal/humanauth"
	"mcp-symposium/internal/storage"
)

// failingProvider always fails authentication, so tests can exercise
// RequireHumanAuth's failure path (redirect-vs-401) without needing a
// real unauthenticated session.
type failingProvider struct{}

func (failingProvider) Authenticate(r *http.Request) (*humanauth.Identity, error) {
	return nil, errors.New("not authenticated")
}

func TestRequireHumanAuth_InjectsActorAndRoleFromStubProvider(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	var gotActorID, gotRole string
	var gotOK, gotRoleOK bool
	handler := humanauth.RequireHumanAuth(db, humanauth.StubProvider{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := humanauth.ActorFromContext(r.Context())
		gotOK = ok
		if ok {
			gotActorID = actor.ID
		}
		gotRole, gotRoleOK = humanauth.RoleFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !gotOK || gotActorID == "" {
		t.Fatal("ActorFromContext() found no actor")
	}
	if !gotRoleOK || gotRole != "admin" {
		t.Errorf("RoleFromContext() = (%q, %v), want (\"admin\", true)", gotRole, gotRoleOK)
	}
}

func TestRequireHumanAuth_ReusesSameActorAcrossRequests(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	var firstID, secondID string
	handler := humanauth.RequireHumanAuth(db, humanauth.StubProvider{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, _ := humanauth.ActorFromContext(r.Context())
		if firstID == "" {
			firstID = actor.ID
		} else {
			secondID = actor.ID
		}
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if firstID == "" || secondID == "" || firstID != secondID {
		t.Errorf("firstID = %q, secondID = %q, want equal and non-empty (same stub identity every request)", firstID, secondID)
	}
}

func TestRequireHumanAuth_RedirectsBrowserNavigationOnAuthFailure(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	handler := humanauth.RequireHumanAuth(db, failingProvider{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not run when auth fails")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect to login)", rec.Code, http.StatusFound)
	}
	if loc := rec.Header().Get("Location"); loc != "/auth/login" {
		t.Errorf("Location = %q, want %q", loc, "/auth/login")
	}
}

func TestRequireHumanAuth_Returns401JSONForNonHTMLRequestsOnAuthFailure(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	handler := humanauth.RequireHumanAuth(db, failingProvider{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not run when auth fails")
	}))

	// The SPA's own fetch() calls don't send an Accept header containing
	// text/html — this must NOT redirect (a redirect here would break the
	// SPA's JSON error handling and risk a redirect loop).
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("Location = %q, want no redirect for a non-HTML request", loc)
	}
}
