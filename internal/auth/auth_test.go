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

// TestRequireBearer_DBErrorIs503Not401 pins the rollout bug from the
// field: a transient DB failure during auth must be 503, not 401 —
// clients treat 401 as "token revoked" and give up for good.
func TestRequireBearer_DBErrorIs503Not401(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	actor, err := storage.CreateAgent(db, "late-agent")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	token, err := auth.Issue(db, actor.ID)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	handler := auth.RequireBearer(db, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Sanity: the token works while the DB is up.
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status with live DB = %d, want 200", rec.Code)
	}

	// Kill the underlying connection: every query now errors (not "not found").
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.Close()

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status with dead DB = %d, want 503 (got a 401 here is the field bug)", rec.Code)
	}

	// And a genuinely bad token is still 401 semantics-wise (on a live DB).
	db2, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	handler2 := auth.RequireBearer(db2, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req2 := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req2.Header.Set("Authorization", "Bearer bogus")
	rec2 := httptest.NewRecorder()
	handler2.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("status with bogus token = %d, want 401", rec2.Code)
	}
}
