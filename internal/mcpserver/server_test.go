package mcpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/mcpserver"
	"mcp-symposium/internal/storage"
	"mcp-symposium/internal/tools/rendezvous"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newTestSession starts an httptest server for db's MCP handler, connects an
// MCP client authenticated as token, and returns the session plus a
// cleanup func that closes both.
func newTestSession(t *testing.T, db *gorm.DB, token string) (*mcp.ClientSession, func()) {
	t.Helper()

	srv := httptest.NewServer(mcpserver.Handler(db, []mcpserver.ToolRegistrar{rendezvous.NewRegistrar(db)}))

	httpClient := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			r.Header.Set("Authorization", "Bearer "+token)
			return http.DefaultTransport.RoundTrip(r)
		}),
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   srv.URL + "/mcp",
		HTTPClient: httpClient,
	}, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("client.Connect() error = %v", err)
	}

	return session, func() {
		session.Close()
		srv.Close()
	}
}

// callTool calls name with args and decodes the tool's structured output
// into out.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, out any) {
	t.Helper()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s) error = %v", name, err)
	}
	if result.IsError {
		t.Fatalf("CallTool(%s) returned a tool error: %+v", name, result.Content)
	}

	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("unmarshal StructuredContent into %T: %v", out, err)
	}
}

func TestCreateThreadTool(t *testing.T) {
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

	session, cleanup := newTestSession(t, db, token)
	defer cleanup()

	var out rendezvous.CreateThreadOutput
	callTool(t, session, "create_thread", map[string]any{
		"title": "New feature X",
		"body":  "Shipped feature X, see docs.",
	}, &out)

	if out.ThreadID == "" {
		t.Fatal("CreateThreadOutput.ThreadID is empty")
	}

	thread, _, _, err := storage.GetThread(db, out.ThreadID)
	if err != nil {
		t.Fatalf("GetThread() error = %v", err)
	}
	if thread.AuthorID != actor.ID {
		t.Errorf("thread.AuthorID = %q, want %q", thread.AuthorID, actor.ID)
	}
}
