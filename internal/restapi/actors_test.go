package restapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcp-symposium/internal/storage"
)

func TestListActorsByID(t *testing.T) {
	db, handler := openTestHandler(t)

	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/actors?ids="+agentA.ID+","+agentB.ID+",nonexistent-id", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /actors status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var actors []storage.Actor
	if err := json.Unmarshal(rec.Body.Bytes(), &actors); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(actors) != 2 {
		t.Fatalf("actors = %+v, want exactly 2 (unknown ID silently omitted)", actors)
	}

	names := map[string]bool{}
	for _, a := range actors {
		names[a.DisplayName] = true
	}
	if !names["agent-a"] || !names["agent-b"] {
		t.Errorf("actors = %+v, want agent-a and agent-b", actors)
	}
}

func TestListActorsByID_EmptyIDsReturnsEmptyArray(t *testing.T) {
	_, handler := openTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/actors", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /actors status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var raw []any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("actors = %+v, want empty array", raw)
	}
}

// TestListActorsByID_JSONKeysAreSnakeCase guards against the missing-json-tags
// bug class already fixed twice in this project (Actor, then
// ListThreadsResult/SearchResult) — decode into a raw map to check the
// actual wire-format keys rather than the Go type, which would mask a tag
// regression via json.Unmarshal's case-insensitive matching.
func TestListActorsByID_JSONKeysAreSnakeCase(t *testing.T) {
	db, handler := openTestHandler(t)

	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/actors?ids="+agent.ID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var raw []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("actors = %+v, want exactly 1", raw)
	}
	for _, key := range []string{"id", "display_name", "kind", "created_at"} {
		if _, ok := raw[0][key]; !ok {
			t.Errorf("response missing key %q (raw response: %v)", key, raw[0])
		}
	}
}
