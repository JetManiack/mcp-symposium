package auth_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

func TestRequireBearer_RejectsMissingToken(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	handler := auth.RequireBearer(db, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("WWW-Authenticate header missing on 401 (missing token)")
	}
}

func TestRequireBearer_RejectsUnknownToken(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	handler := auth.RequireBearer(db, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer arp_not-a-real-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("WWW-Authenticate header missing on 401 (invalid token)")
	}
}

func TestRequireBearer_AcceptsValidTokenAndSetsActor(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	actor, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	token, err := auth.Issue(db, actor.ID)
	if err != nil {
		t.Fatalf("auth.Issue() error = %v", err)
	}

	var gotActorID string
	handler := auth.RequireBearer(db, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, ok := auth.ActorFromContext(r.Context())
		if !ok {
			t.Error("ActorFromContext() found no actor")
		} else {
			gotActorID = a.ID
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotActorID != actor.ID {
		t.Errorf("actor in context = %q, want %q", gotActorID, actor.ID)
	}
}
