package restapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	"mcp-symposium/internal/humanauth"
	"mcp-symposium/internal/restapi"
	"mcp-symposium/internal/storage"
)

type viewerStubProvider struct{}

func (viewerStubProvider) Authenticate(r *http.Request) (*humanauth.Identity, error) {
	return &humanauth.Identity{Subject: "viewer-stub", DisplayName: "Viewer Tester", Role: "viewer"}, nil
}

func openTestHandlerAsViewer(t *testing.T) (*gorm.DB, http.Handler) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return db, restapi.NewHandler(db, viewerStubProvider{})
}

func TestRoleGating_ViewerCanReadThreadsAndSearch(t *testing.T) {
	_, handler := openTestHandlerAsViewer(t)

	for _, path := range []string{"/threads", "/search?q=x", "/me", "/actors"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s as viewer status = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}

// TestRoleGating_ViewerCanReadThreadDetail guards a route that's easy to
// silently over-lock (a future task wrapping GET /threads/{id} in
// RequireAdmin would break every viewer's ability to read threads, but
// TestRoleGating_ViewerCanReadThreadsAndSearch's plain "/threads" only
// covers the list endpoint, not the detail one).
func TestRoleGating_ViewerCanReadThreadDetail(t *testing.T) {
	db, adminHandler := openTestHandler(t)

	createReq := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(`{"title":"x","body":"y"}`))
	createRec := httptest.NewRecorder()
	adminHandler.ServeHTTP(createRec, createReq)
	var created storage.Thread
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	viewerHandler := restapi.NewHandler(db, viewerStubProvider{})
	req := httptest.NewRequest(http.MethodGet, "/threads/"+created.ID, nil)
	rec := httptest.NewRecorder()
	viewerHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /threads/:id as viewer status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRoleGating_ViewerCannotCreateThread(t *testing.T) {
	_, handler := openTestHandlerAsViewer(t)

	req := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(`{"title":"x","body":"y"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /threads as viewer status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRoleGating_ViewerCannotReplyOrChangeStatus(t *testing.T) {
	db, adminHandler := openTestHandler(t)

	createReq := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(`{"title":"x","body":"y"}`))
	createRec := httptest.NewRecorder()
	adminHandler.ServeHTTP(createRec, createReq)
	var created storage.Thread
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	viewerHandler := restapi.NewHandler(db, viewerStubProvider{})

	replyReq := httptest.NewRequest(http.MethodPost, "/threads/"+created.ID+"/replies", strings.NewReader(`{"body":"hi"}`))
	replyRec := httptest.NewRecorder()
	viewerHandler.ServeHTTP(replyRec, replyReq)
	if replyRec.Code != http.StatusForbidden {
		t.Errorf("POST reply as viewer status = %d, want %d", replyRec.Code, http.StatusForbidden)
	}

	statusReq := httptest.NewRequest(http.MethodPatch, "/threads/"+created.ID, strings.NewReader(`{"status":"resolved"}`))
	statusRec := httptest.NewRecorder()
	viewerHandler.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusForbidden {
		t.Errorf("PATCH status as viewer status = %d, want %d", statusRec.Code, http.StatusForbidden)
	}
}

func TestRoleGating_ViewerCannotAccessAgents(t *testing.T) {
	_, handler := openTestHandlerAsViewer(t)

	req := httptest.NewRequest(http.MethodGet, "/actors/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /actors/ as viewer status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRoleGating_AdminCanAccessEverything(t *testing.T) {
	_, handler := openTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/actors/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /actors/ as admin status = %d, want %d", rec.Code, http.StatusOK)
	}
}
