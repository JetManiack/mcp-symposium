package mcpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/mcpserver"
	"mcp-symposium/internal/storage"
	"mcp-symposium/internal/tools/rendezvous"
)

// newTestSessionWithResourceUpdates behaves like newTestSession
// (server_test.go) but also captures every "notifications/resources/updated"
// URI the server sends, via a buffered channel — for tests that observe
// push notifications rather than just call tools. Deliberately not folded
// into newTestSession itself, to avoid touching every existing tool test.
func newTestSessionWithResourceUpdates(t *testing.T, db *gorm.DB, token string) (*mcp.ClientSession, chan string, func()) {
	t.Helper()

	srv := httptest.NewServer(mcpserver.Handler(db, []mcpserver.ToolRegistrar{rendezvous.NewRegistrar(db)}))

	httpClient := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			r.Header.Set("Authorization", "Bearer "+token)
			return http.DefaultTransport.RoundTrip(r)
		}),
	}

	updates := make(chan string, 16)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, &mcp.ClientOptions{
		ResourceUpdatedHandler: func(ctx context.Context, req *mcp.ResourceUpdatedNotificationRequest) {
			updates <- req.Params.URI
		},
	})
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   srv.URL + "/mcp",
		HTTPClient: httpClient,
	}, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("client.Connect() error = %v", err)
	}

	return session, updates, func() {
		session.Close()
		srv.Close()
	}
}

// newSharedTestServer starts one httptest server for db's MCP handler and
// returns its URL, closing it via t.Cleanup. Tests with multiple sessions
// that must observe each other's push notifications (state that lives in
// the *mcp.Server itself, not the database) must connect every session to
// the SAME server — never call this once per session.
func newSharedTestServer(t *testing.T, db *gorm.DB) string {
	t.Helper()
	srv := httptest.NewServer(mcpserver.Handler(db, []mcpserver.ToolRegistrar{rendezvous.NewRegistrar(db)}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// connectResourceUpdateSession connects an MCP client (authenticated as
// token) to serverURL — from newSharedTestServer, so multiple sessions can
// share one *mcp.Server — and captures every "notifications/resources/updated"
// URI the server sends, via a buffered channel.
func connectResourceUpdateSession(t *testing.T, serverURL, token string) (*mcp.ClientSession, chan string, func()) {
	t.Helper()

	httpClient := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			r.Header.Set("Authorization", "Bearer "+token)
			return http.DefaultTransport.RoundTrip(r)
		}),
	}

	updates := make(chan string, 16)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, &mcp.ClientOptions{
		ResourceUpdatedHandler: func(ctx context.Context, req *mcp.ResourceUpdatedNotificationRequest) {
			updates <- req.Params.URI
		},
	})
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   serverURL + "/mcp",
		HTTPClient: httpClient,
	}, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}

	return session, updates, func() { session.Close() }
}

func TestCatchUpTool_IncludesActorID(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	token, err := auth.Issue(db,agent.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken() error = %v", err)
	}

	session, cleanup := newTestSession(t, db, token)
	defer cleanup()

	var out rendezvous.CatchUpOutput
	callTool(t, session, "catch_up", map[string]any{}, &out)

	if out.ActorID != agent.ID {
		t.Errorf("ActorID = %q, want %q", out.ActorID, agent.ID)
	}
}

func TestSubscribeToOwnCatchUpResource_Succeeds(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	token, err := auth.Issue(db,agent.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken() error = %v", err)
	}

	session, _, cleanup := newTestSessionWithResourceUpdates(t, db, token)
	defer cleanup()

	if err := session.Subscribe(context.Background(), &mcp.SubscribeParams{URI: "rendezvous://catchup/" + agent.ID}); err != nil {
		t.Fatalf("Subscribe() to own resource error = %v", err)
	}
}

// TestCatchUpResourceTemplate_IsListedAsATemplateNotAConcreteResource
// documents intended behavior that was flagged as a possible bug during
// the push-notification investigation (board thread 4f93edd4): a valid
// subscribe on rendezvous://catchup/{actorID} succeeds, yet
// resources/list comes back empty. That's correct per the MCP spec —
// resources/list only lists concrete resources; a URI *template* like
// ours is listed via the separate resources/templates/list. Confirming
// it here means the resolver already validates correctly and nothing
// about subscribe/list needs to change.
func TestCatchUpResourceTemplate_IsListedAsATemplateNotAConcreteResource(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	token, err := auth.Issue(db,agent.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken() error = %v", err)
	}

	session, cleanup := newTestSession(t, db, token)
	defer cleanup()

	resources, err := session.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources.Resources) != 0 {
		t.Errorf("ListResources() = %v, want empty (the catch-up feed is a template, not a concrete resource)", resources.Resources)
	}

	templates, err := session.ListResourceTemplates(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListResourceTemplates() error = %v", err)
	}
	found := false
	for _, tpl := range templates.ResourceTemplates {
		if tpl.URITemplate == "rendezvous://catchup/{actorID}" {
			found = true
		}
	}
	if !found {
		t.Errorf("ListResourceTemplates() = %v, want it to include rendezvous://catchup/{actorID}", templates.ResourceTemplates)
	}
}

func TestSubscribeToUnknownURIScheme_Fails(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	token, err := auth.Issue(db,agent.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken() error = %v", err)
	}

	session, cleanup := newTestSession(t, db, token)
	defer cleanup()

	err = session.Subscribe(context.Background(), &mcp.SubscribeParams{URI: "not-a-catchup-uri://something"})
	if err == nil {
		t.Fatal("Subscribe() to an unrelated URI scheme succeeded, want an error")
	}
}

func TestSubscribeToAnotherActorsCatchUpResource_Fails(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	tokenA, err := auth.Issue(db,agentA.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken(agent-a) error = %v", err)
	}

	session, _, cleanup := newTestSessionWithResourceUpdates(t, db, tokenA)
	defer cleanup()

	err = session.Subscribe(context.Background(), &mcp.SubscribeParams{URI: "rendezvous://catchup/" + agentB.ID})
	if err == nil {
		t.Fatal("Subscribe() to another actor's resource succeeded, want an error")
	}
}

func TestReadOwnCatchUpResource_ReturnsSummaryWithoutMarkingSeen(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	tokenB, err := auth.Issue(db,agentB.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken(agent-b) error = %v", err)
	}
	if _, err := storage.CreateThread(db, agentA.ID, "Deploy", "Deploying, cc @agent-b", nil); err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	session, _, cleanup := newTestSessionWithResourceUpdates(t, db, tokenB)
	defer cleanup()

	result, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "rendezvous://catchup/" + agentB.ID})
	if err != nil {
		t.Fatalf("ReadResource() error = %v", err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("len(Contents) = %d, want 1", len(result.Contents))
	}
	var summary storage.CatchUpSummary
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &summary); err != nil {
		t.Fatalf("unmarshal resource contents: %v", err)
	}
	if summary.UnseenMentionCount != 1 {
		t.Errorf("UnseenMentionCount = %d, want 1", summary.UnseenMentionCount)
	}

	// Reading the resource must not have marked the mention seen.
	var out rendezvous.CatchUpOutput
	callTool(t, session, "catch_up", map[string]any{}, &out)
	if len(out.NewMentions) != 1 {
		t.Errorf("catch_up NewMentions after a resource read = %d, want 1 (resource read must be non-destructive)", len(out.NewMentions))
	}
}

func TestAddReply_NotifiesWatcherButNotAuthor(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	tokenA, err := auth.Issue(db,agentA.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken(agent-a) error = %v", err)
	}
	tokenB, err := auth.Issue(db,agentB.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken(agent-b) error = %v", err)
	}

	serverURL := newSharedTestServer(t, db)
	sessionA, updatesA, cleanupA := connectResourceUpdateSession(t, serverURL, tokenA)
	defer cleanupA()
	sessionB, updatesB, cleanupB := connectResourceUpdateSession(t, serverURL, tokenB)
	defer cleanupB()

	if err := sessionA.Subscribe(context.Background(), &mcp.SubscribeParams{URI: "rendezvous://catchup/" + agentA.ID}); err != nil {
		t.Fatalf("agent-a Subscribe() error = %v", err)
	}
	if err := sessionB.Subscribe(context.Background(), &mcp.SubscribeParams{URI: "rendezvous://catchup/" + agentB.ID}); err != nil {
		t.Fatalf("agent-b Subscribe() error = %v", err)
	}

	var created rendezvous.CreateThreadOutput
	callTool(t, sessionA, "create_thread", map[string]any{
		"title": "Deploy",
		"body":  "Deploying now",
	}, &created)

	var watched rendezvous.WatchThreadOutput
	callTool(t, sessionB, "watch_thread", map[string]any{
		"thread_id": created.ThreadID,
	}, &watched)

	var replied rendezvous.ReplyOutput
	callTool(t, sessionA, "reply", map[string]any{
		"thread_id": created.ThreadID,
		"body":      "Update: still going",
	}, &replied)

	select {
	case uri := <-updatesB:
		want := "rendezvous://catchup/" + agentB.ID
		if uri != want {
			t.Errorf("agent-b notified URI = %q, want %q", uri, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent-b (a watcher) was not notified of the new reply")
	}

	select {
	case uri := <-updatesA:
		t.Errorf("agent-a (the replier) was unexpectedly notified: %q", uri)
	case <-time.After(200 * time.Millisecond):
		// Expected: the author of its own reply gets no notification.
	}
}

func TestCreateThread_NotifiesMentionedActor(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	tokenA, err := auth.Issue(db,agentA.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken(agent-a) error = %v", err)
	}
	tokenB, err := auth.Issue(db,agentB.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken(agent-b) error = %v", err)
	}

	serverURL := newSharedTestServer(t, db)
	sessionA, _, cleanupA := connectResourceUpdateSession(t, serverURL, tokenA)
	defer cleanupA()
	sessionB, updatesB, cleanupB := connectResourceUpdateSession(t, serverURL, tokenB)
	defer cleanupB()

	if err := sessionB.Subscribe(context.Background(), &mcp.SubscribeParams{URI: "rendezvous://catchup/" + agentB.ID}); err != nil {
		t.Fatalf("agent-b Subscribe() error = %v", err)
	}

	var created rendezvous.CreateThreadOutput
	callTool(t, sessionA, "create_thread", map[string]any{
		"title": "Deploy",
		"body":  "Deploying now, cc @agent-b",
	}, &created)

	select {
	case uri := <-updatesB:
		want := "rendezvous://catchup/" + agentB.ID
		if uri != want {
			t.Errorf("agent-b notified URI = %q, want %q", uri, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent-b (mentioned in the thread body) was not notified")
	}
}
