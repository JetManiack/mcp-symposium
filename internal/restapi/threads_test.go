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

// openTestHandler is shared by every *_test.go file in this package (all
// in package restapi_test) — defined once here in threads_test.go.
func openTestHandler(t *testing.T) (*gorm.DB, http.Handler) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return db, restapi.NewHandler(db, humanauth.StubProvider{})
}

func TestCreateAndListThreads(t *testing.T) {
	_, handler := openTestHandler(t)

	createReq := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(
		`{"title":"Deploy","body":"Shipping now","tags":["deploy"]}`,
	))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST /threads status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	var created storage.Thread
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	if created.Title != "Deploy" {
		t.Errorf("Title = %q, want %q", created.Title, "Deploy")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/threads", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /threads status = %d", listRec.Code)
	}
	var result storage.ListThreadsResult
	if err := json.Unmarshal(listRec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if len(result.Threads) != 1 || result.Threads[0].ID != created.ID {
		t.Fatalf("Threads = %+v, want exactly the created thread", result.Threads)
	}
}

// TestListThreads_JSONKeysAreSnakeCase guards against a regression where
// storage.ListThreadsResult had no json tags: json.Unmarshal is
// case-insensitive, so decoding straight into storage.ListThreadsResult
// (as TestCreateAndListThreads above does) silently accepted capitalized
// Go field names and masked the bug — but a browser's case-sensitive
// property access (result.threads, result.next_cursor) saw undefined.
// Decoding into a raw map surfaces the actual wire-format keys.
func TestListThreads_JSONKeysAreSnakeCase(t *testing.T) {
	_, handler := openTestHandler(t)

	createReq := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(
		`{"title":"Deploy","body":"Shipping now"}`,
	))
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	listReq := httptest.NewRequest(http.MethodGet, "/threads", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)

	var raw map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}

	if _, ok := raw["threads"]; !ok {
		t.Errorf("response missing key %q (raw response: %v)", "threads", raw)
	}
	if _, ok := raw["next_cursor"]; !ok {
		t.Errorf("response missing key %q (raw response: %v)", "next_cursor", raw)
	}
}

func TestGetThreadAndReplyAndUpdateStatus(t *testing.T) {
	_, handler := openTestHandler(t)

	createReq := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(
		`{"title":"Deploy","body":"Shipping now"}`,
	))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	var created storage.Thread
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	replyReq := httptest.NewRequest(http.MethodPost, "/threads/"+created.ID+"/replies", strings.NewReader(
		`{"body":"Hit a bug"}`,
	))
	replyRec := httptest.NewRecorder()
	handler.ServeHTTP(replyRec, replyReq)
	if replyRec.Code != http.StatusCreated {
		t.Fatalf("POST /threads/:id/replies status = %d, body = %s", replyRec.Code, replyRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/threads/"+created.ID, nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /threads/:id status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	var got struct {
		Thread  storage.Thread  `json:"thread"`
		Replies []storage.Reply `json:"replies"`
		Tags    []string        `json:"tags"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal get response: %v", err)
	}
	if len(got.Replies) != 1 || got.Replies[0].Body != "Hit a bug" {
		t.Fatalf("Replies = %+v, want exactly the one reply", got.Replies)
	}

	statusReq := httptest.NewRequest(http.MethodPatch, "/threads/"+created.ID, strings.NewReader(
		`{"status":"resolved"}`,
	))
	statusRec := httptest.NewRecorder()
	handler.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("PATCH /threads/:id status = %d, body = %s", statusRec.Code, statusRec.Body.String())
	}
	var updated storage.Thread
	if err := json.Unmarshal(statusRec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal patch response: %v", err)
	}
	if updated.Status != "resolved" {
		t.Errorf("Status = %q, want %q", updated.Status, "resolved")
	}
}

// TestListThreads_EmptyResultIsArrayNotNull guards against a regression
// where storage.ListThreadsResult.Threads is left as a nil Go slice when
// zero threads match (GORM's Find leaves an unfilled slice nil rather than
// allocating an empty one), and the REST handler serialized it verbatim.
// A JS client decodes `null` where it expects an array, then a `.map()`
// or `.length` on it throws and blanks the whole page — exactly what
// happened with GET /threads returning no results under a status filter.
func TestListThreads_EmptyResultIsArrayNotNull(t *testing.T) {
	_, handler := openTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/threads?status=resolved", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /threads?status=resolved status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	threads, ok := raw["threads"].([]any)
	if !ok {
		t.Fatalf(`"threads" = %#v (type %T), want a JSON array (got null or wrong type)`, raw["threads"], raw["threads"])
	}
	if len(threads) != 0 {
		t.Errorf("threads = %v, want empty", threads)
	}
}

func TestCreateThread_RejectsEmptyTitleOrBody(t *testing.T) {
	_, handler := openTestHandler(t)

	cases := []string{
		`{"title":"","body":"a body"}`,
		`{"title":"   ","body":"a body"}`,
		`{"title":"a title","body":""}`,
		`{"title":"a title","body":"  \t"}`,
	}
	for _, payload := range cases {
		req := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(payload))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST /threads %s status = %d, want %d (body = %s)", payload, rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	}
}

func TestAddReply_RejectsEmptyBody(t *testing.T) {
	_, handler := openTestHandler(t)

	createReq := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(
		`{"title":"Deploy","body":"Shipping now"}`,
	))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	var created storage.Thread
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	for _, payload := range []string{`{"body":""}`, `{"body":"   "}`} {
		req := httptest.NewRequest(http.MethodPost, "/threads/"+created.ID+"/replies", strings.NewReader(payload))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST /threads/:id/replies %s status = %d, want %d (body = %s)", payload, rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	}
}

func TestUpdateThreadStatus_RejectsInvalidStatus(t *testing.T) {
	_, handler := openTestHandler(t)

	createReq := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(
		`{"title":"Deploy","body":"Shipping now"}`,
	))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	var created storage.Thread
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	statusReq := httptest.NewRequest(http.MethodPatch, "/threads/"+created.ID, strings.NewReader(
		`{"status":"archived"}`,
	))
	statusRec := httptest.NewRecorder()
	handler.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH /threads/:id (invalid status) = %d, want %d", statusRec.Code, http.StatusBadRequest)
	}
}
