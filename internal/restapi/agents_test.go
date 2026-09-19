package restapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

func TestCreateListAndDeleteAgent(t *testing.T) {
	_, handler := openTestHandler(t)

	createReq := httptest.NewRequest(http.MethodPost, "/actors/", strings.NewReader(
		`{"display_name":"deploy-bot"}`,
	))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST /actors/ status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	var created storage.Actor
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	if created.DisplayName != "deploy-bot" {
		t.Errorf("DisplayName = %q, want %q", created.DisplayName, "deploy-bot")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/actors/", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	var agents []storage.Actor
	if err := json.Unmarshal(listRec.Body.Bytes(), &agents); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if len(agents) != 1 || agents[0].ID != created.ID {
		t.Fatalf("agents = %+v, want exactly the created agent", agents)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/actors/"+created.ID, nil)
	deleteRec := httptest.NewRecorder()
	handler.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /actors/:id status = %d, body = %s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestIssueAndRevokeToken(t *testing.T) {
	db, handler := openTestHandler(t)

	agent, err := storage.CreateAgent(db, "deploy-bot")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	issueReq := httptest.NewRequest(http.MethodPost, "/actors/"+agent.ID+"/credentials", nil)
	issueRec := httptest.NewRecorder()
	handler.ServeHTTP(issueRec, issueReq)
	if issueRec.Code != http.StatusCreated {
		t.Fatalf("POST /actors/:id/credentials status = %d, body = %s", issueRec.Code, issueRec.Body.String())
	}
	var issued struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(issueRec.Body.Bytes(), &issued); err != nil {
		t.Fatalf("unmarshal issue response: %v", err)
	}
	if issued.Token == "" {
		t.Fatal("issued token is empty")
	}

	authenticated, err := auth.Authenticate(db, issued.Token)
	if err != nil {
		t.Fatalf("auth.Authenticate() error = %v", err)
	}
	if authenticated.ID != agent.ID {
		t.Errorf("authenticated.ID = %q, want %q", authenticated.ID, agent.ID)
	}

	var cred storage.AgentCredential
	if err := db.First(&cred, "actor_id = ?", agent.ID).Error; err != nil {
		t.Fatalf("First(credential) error = %v", err)
	}

	revokeReq := httptest.NewRequest(http.MethodDelete, "/credentials/"+cred.ID, nil)
	revokeRec := httptest.NewRecorder()
	handler.ServeHTTP(revokeRec, revokeReq)
	if revokeRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /credentials/:id status = %d, body = %s", revokeRec.Code, revokeRec.Body.String())
	}

	if _, err := auth.Authenticate(db, issued.Token); err != storage.ErrInvalidToken {
		t.Errorf("auth.Authenticate() after revoke error = %v, want %v", err, storage.ErrInvalidToken)
	}
}

// TestCreateAgent_JSONKeysAreSnakeCase guards against a regression where
// Actor had no json tags: json.Unmarshal is case-insensitive, so decoding
// straight into storage.Actor silently accepted capitalized Go field names
// and masked the bug — but a browser's case-sensitive property access
// (agent.id, agent.display_name) saw undefined. Decoding into a raw map
// surfaces the actual wire-format keys the way a JS client would see them.
func TestCreateAgent_JSONKeysAreSnakeCase(t *testing.T) {
	_, handler := openTestHandler(t)

	createReq := httptest.NewRequest(http.MethodPost, "/actors/", strings.NewReader(
		`{"display_name":"deploy-bot"}`,
	))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST /actors/ status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	for _, key := range []string{"id", "display_name", "kind", "created_at"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("response missing key %q (raw response: %v)", key, raw)
		}
	}
}

// TestListAgents_ReportsActiveTokenStatus guards the "revoke, don't
// delete" contract: DELETE /agents/:id revokes credentials but keeps the
// Actor row (thread/reply history survives), so the list must expose
// whether an agent currently has a usable token — the Web UI badges
// revoked agents instead of removing them from the list.
func TestListAgents_ReportsActiveTokenStatus(t *testing.T) {
	_, handler := openTestHandler(t)

	createReq := httptest.NewRequest(http.MethodPost, "/actors/", strings.NewReader(
		`{"display_name":"deploy-bot"}`,
	))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	var created storage.Actor
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	assertHasActiveToken := func(want bool) {
		t.Helper()
		listReq := httptest.NewRequest(http.MethodGet, "/actors/", nil)
		listRec := httptest.NewRecorder()
		handler.ServeHTTP(listRec, listReq)
		var raw []map[string]any
		if err := json.Unmarshal(listRec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("unmarshal list response: %v", err)
		}
		if len(raw) != 1 {
			t.Fatalf("agents = %+v, want exactly 1", raw)
		}
		got, ok := raw[0]["has_active_token"].(bool)
		if !ok {
			t.Fatalf("has_active_token missing or not bool in %+v", raw[0])
		}
		if got != want {
			t.Errorf("has_active_token = %v, want %v", got, want)
		}
	}

	assertHasActiveToken(false)

	issueReq := httptest.NewRequest(http.MethodPost, "/actors/"+created.ID+"/credentials", nil)
	issueRec := httptest.NewRecorder()
	handler.ServeHTTP(issueRec, issueReq)
	if issueRec.Code != http.StatusCreated {
		t.Fatalf("POST /actors/:id/credentials status = %d, body = %s", issueRec.Code, issueRec.Body.String())
	}

	assertHasActiveToken(true)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/actors/"+created.ID, nil)
	deleteRec := httptest.NewRecorder()
	handler.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /actors/:id status = %d, body = %s", deleteRec.Code, deleteRec.Body.String())
	}

	assertHasActiveToken(false)
}

func TestSearch_FindsCreatedThread(t *testing.T) {
	_, handler := openTestHandler(t)

	createReq := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(
		`{"title":"Deploy pipeline broken","body":"The nightly deploy pipeline is failing."}`,
	))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	var created storage.Thread
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	searchReq := httptest.NewRequest(http.MethodGet, "/search?q=pipeline", nil)
	searchRec := httptest.NewRecorder()
	handler.ServeHTTP(searchRec, searchReq)
	if searchRec.Code != http.StatusOK {
		t.Fatalf("GET /search status = %d, body = %s", searchRec.Code, searchRec.Body.String())
	}
	var result storage.SearchResult
	if err := json.Unmarshal(searchRec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	if len(result.Threads) != 1 || result.Threads[0].ID != created.ID {
		t.Fatalf("Threads = %+v, want exactly the created thread", result.Threads)
	}
}

// TestSearch_EmptyResultIsArrayNotNull guards against a regression where
// storage.SearchResult's Threads/Replies are left as nil Go slices when
// nothing matches, and the REST handler serialized them verbatim as JSON
// null. A JS client's `replies.map(...)` on `null` throws and blanks the
// whole page — this was the actual reported bug behind "search returns a
// blank page."
func TestSearch_EmptyResultIsArrayNotNull(t *testing.T) {
	_, handler := openTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/search?q=nonexistent-keyword-xyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /search status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	for _, key := range []string{"threads", "replies"} {
		val, ok := raw[key].([]any)
		if !ok {
			t.Errorf(`%q = %#v (type %T), want a JSON array (got null or wrong type)`, key, raw[key], raw[key])
			continue
		}
		if len(val) != 0 {
			t.Errorf("%s = %v, want empty", key, val)
		}
	}
}

// TestSearch_JSONKeysAreSnakeCase guards against a regression where
// storage.SearchResult had no json tags — see
// TestListThreads_JSONKeysAreSnakeCase in threads_test.go for why decoding
// straight into the storage type (as TestSearch_FindsCreatedThread above
// does) can't catch this class of bug.
func TestSearch_JSONKeysAreSnakeCase(t *testing.T) {
	_, handler := openTestHandler(t)

	createReq := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(
		`{"title":"Deploy pipeline broken","body":"The nightly deploy pipeline is failing."}`,
	))
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	searchReq := httptest.NewRequest(http.MethodGet, "/search?q=pipeline", nil)
	searchRec := httptest.NewRecorder()
	handler.ServeHTTP(searchRec, searchReq)

	var raw map[string]any
	if err := json.Unmarshal(searchRec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}

	if _, ok := raw["threads"]; !ok {
		t.Errorf("response missing key %q (raw response: %v)", "threads", raw)
	}
	if _, ok := raw["replies"]; !ok {
		t.Errorf("response missing key %q (raw response: %v)", "replies", raw)
	}
}

func TestSearch_RejectsUnsupportedMode(t *testing.T) {
	_, handler := openTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/search?q=pipeline&mode=semantic", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /search?mode=semantic status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
