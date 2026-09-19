package restapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcp-symposium/internal/storage"
)

func TestPutMeProfile_UpsertsOwnProfileForAnyRole(t *testing.T) {
	_, handler := openTestHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/me/profile", strings.NewReader(
		`{"name":"Stub User","nickname":"stub-nick","bio":"testing","tags":["qa"]}`,
	))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /me/profile status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Nickname string   `json:"nickname"`
		Tags     []string `json:"tags"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Nickname != "stub-nick" {
		t.Errorf("Nickname = %q, want %q", resp.Nickname, "stub-nick")
	}
	if len(resp.Tags) != 1 || resp.Tags[0] != "qa" {
		t.Errorf("Tags = %v, want [qa]", resp.Tags)
	}
}

func TestGetProfile_ReadableByAnyAuthenticatedRole(t *testing.T) {
	db, handler := openTestHandler(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	if _, err := storage.UpsertActorProfile(db, agent.ID, "Agent A", "agent-a-nick", "bio", nil); err != nil {
		t.Fatalf("UpsertActorProfile() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/profiles/"+agent.ID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profiles/{id} status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Nickname string `json:"nickname"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Nickname != "agent-a-nick" {
		t.Errorf("Nickname = %q, want %q", resp.Nickname, "agent-a-nick")
	}
}

func TestPutProfileByID_UpdatesAnyProfile_AdminOnly(t *testing.T) {
	db, handler := openTestHandler(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/profiles/"+agent.ID, strings.NewReader(
		`{"name":"Agent A","nickname":"agent-a-nick","bio":"set by admin","tags":[]}`,
	))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /profiles/{id} status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
