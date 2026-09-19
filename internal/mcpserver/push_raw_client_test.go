package mcpserver_test

// This file drives the MCP streamable-HTTP transport with plain net/http
// calls instead of mcp.StreamableClientTransport, to close a real
// coverage gap: resource_catchup_test.go's tests all use the SDK's own
// client on both ends, which — per the live investigation in board
// thread 4f93edd4 — may wire up client/server delivery in a way that a
// genuine standing GET stream from a non-SDK client doesn't get for
// free. These tests are a faithful, byte-level reproduction of the raw
// curl sequence Claude-on-k8s ran against the live pod (initialize ->
// notifications/initialized -> resources/subscribe -> hold GET -> a
// second actor triggers), including both trigger paths tested live
// (mention and watched-thread reply) and a realistic delay before
// firing.
//
// Faithful as they are to the protocol, these two could not reproduce
// the live failure, because the protocol was never the variable: they run
// against httptest.NewServer, whose *http.Server has every timeout at
// zero, while production sets WriteTimeout. That one config difference
// was the whole bug —
// TestPushNotification_DelayedPushSurvivesServerWriteTimeout below is the
// case that catches it, and the comment on
// mcpserver.withoutWriteDeadline explains why the failure is silent on
// both ends.
//
// The lesson worth keeping: a transport-level test that stands up its
// server with different timeouts than production is testing a different
// server. Configure the harness like production, or it will stay blind in
// exactly the place production differs.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/mcpserver"
	"mcp-symposium/internal/storage"
	"mcp-symposium/internal/tools/rendezvous"
)

// newRawTestServer starts an httptest server whose *http.Server can be
// configured before it accepts connections. httptest.NewServer leaves
// every timeout at zero, which is exactly the production-only
// configuration the tests in this file were blind to (see
// TestPushNotification_DelayedPushSurvivesServerWriteTimeout).
func newRawTestServer(t *testing.T, h http.Handler, configure func(*http.Server)) *httptest.Server {
	t.Helper()

	srv := httptest.NewUnstartedServer(h)
	if configure != nil {
		configure(srv.Config)
	}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

// rawRPC issues a single JSON-RPC POST against base and returns the
// decoded response body (nil for a notification, which has none) along
// with any Mcp-Session-Id the server assigned (only present on the
// initialize response).
func rawRPC(t *testing.T, base, token, sessionID string, body map[string]any) (resp map[string]any, newSessionID string) {
	t.Helper()

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, base+"/mcp", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %v: %v", body["method"], err)
	}
	defer httpResp.Body.Close()

	respData, err := io.ReadAll(httpResp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if httpResp.StatusCode != http.StatusOK && httpResp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST %v: status %d, body %s", body["method"], httpResp.StatusCode, respData)
	}

	newSessionID = httpResp.Header.Get("Mcp-Session-Id")
	payload := extractJSONPayload(strings.TrimSpace(string(respData)))
	if payload == "" {
		return nil, newSessionID
	}
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", respData, err)
	}
	return resp, newSessionID
}

// extractJSONPayload returns the JSON-RPC message from a POST response
// body, which the server may send either as a bare JSON object or as a
// single SSE frame ("event: message\ndata: {...}\n\n") depending on its
// jsonResponse mode. Empty input (a notification has no response body)
// returns "".
func extractJSONPayload(body string) string {
	if body == "" {
		return ""
	}
	if body[0] == '{' {
		return body
	}
	for _, line := range strings.Split(body, "\n") {
		if data, ok := strings.CutPrefix(line, "data:"); ok {
			return strings.TrimSpace(data)
		}
	}
	return ""
}

// openStandingStream holds a real GET /mcp SSE connection open — the
// standalone stream a client uses to receive server-initiated messages —
// and streams every "data:" line it receives onto the returned channel.
// The returned cancel func must be called to release the connection.
func openStandingStream(t *testing.T, base, token, sessionID string) (events <-chan string, cancel func()) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, base+"/mcp", nil)
	if err != nil {
		t.Fatalf("NewRequest GET: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /mcp: status %d", resp.StatusCode)
	}

	ch := make(chan string, 16)
	done := make(chan struct{})
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if data, ok := strings.CutPrefix(line, "data:"); ok {
				select {
				case ch <- strings.TrimSpace(data):
				case <-done:
					return
				}
			}
		}
	}()

	return ch, func() {
		close(done)
		resp.Body.Close()
	}
}

func initializeRawSession(t *testing.T, base, token string) string {
	t.Helper()

	_, sessionID := rawRPC(t, base, token, "", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "raw-repro-client", "version": "1.0"},
		},
	})
	if sessionID == "" {
		t.Fatalf("initialize did not return a %s header", "Mcp-Session-Id")
	}

	rawRPC(t, base, token, sessionID, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})

	return sessionID
}

func TestPushNotification_RealStandingGETStream_ReceivesResourceUpdated(t *testing.T) {
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
	tokenA, err := auth.Issue(db, agentA.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken(agent-a) error = %v", err)
	}
	tokenB, err := auth.Issue(db, agentB.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken(agent-b) error = %v", err)
	}

	srv := httptest.NewServer(mcpserver.Handler(db, []mcpserver.ToolRegistrar{rendezvous.NewRegistrar(db)}))
	defer srv.Close()

	// agent B: initialize, subscribe to its own catch-up feed, then hold
	// a real standing GET stream open — no SDK client on either end.
	sessionB := initializeRawSession(t, srv.URL, tokenB)

	subResp, _ := rawRPC(t, srv.URL, tokenB, sessionB, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "resources/subscribe",
		"params":  map[string]any{"uri": "rendezvous://catchup/" + agentB.ID},
	})
	if _, ok := subResp["result"]; !ok {
		t.Fatalf("resources/subscribe did not succeed: %v", subResp)
	}

	events, cancel := openStandingStream(t, srv.URL, tokenB, sessionB)
	defer cancel()

	// agent A: separate session, mentions agent B in a new thread — the
	// create_thread -> mention -> notify path with the most moving parts.
	sessionA := initializeRawSession(t, srv.URL, tokenA)
	callResp, _ := rawRPC(t, srv.URL, tokenA, sessionA, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "create_thread",
			"arguments": map[string]any{
				"title": "Push repro",
				"body":  "cc @agent-b",
			},
		},
	})
	if _, ok := callResp["result"]; !ok {
		t.Fatalf("create_thread call did not succeed: %v", callResp)
	}

	select {
	case ev := <-events:
		t.Logf("received on standing GET stream: %s", ev)
	case <-time.After(5 * time.Second):
		t.Fatal("no notification arrived on the real standing GET stream within 5s")
	}
}

// TestPushNotification_WatcherPathWithRealisticDelay_RawClient exercises
// the other trigger path from the live investigation (reply to a watched
// thread, no mention involved) and adds a deliberate gap between holding
// the stream and firing the trigger, since the live sessions in board
// thread 4f93edd4 were held for minutes before triggering, not
// milliseconds. If this ever goes red while
// TestPushNotification_RealStandingGETStream_ReceivesResourceUpdated
// stays green, the two results together would localize a regression to
// the watcher path specifically rather than delivery in general.
func TestPushNotification_WatcherPathWithRealisticDelay_RawClient(t *testing.T) {
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
	tokenA, err := auth.Issue(db, agentA.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken(agent-a) error = %v", err)
	}
	tokenB, err := auth.Issue(db, agentB.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken(agent-b) error = %v", err)
	}

	srv := httptest.NewServer(mcpserver.Handler(db, []mcpserver.ToolRegistrar{rendezvous.NewRegistrar(db)}))
	defer srv.Close()

	sessionA := initializeRawSession(t, srv.URL, tokenA)
	createResp, _ := rawRPC(t, srv.URL, tokenA, sessionA, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "create_thread",
			"arguments": map[string]any{"title": "Watched thread", "body": "no mentions here"},
		},
	})
	threadID := extractThreadID(t, createResp)

	sessionB := initializeRawSession(t, srv.URL, tokenB)
	if watchResp, _ := rawRPC(t, srv.URL, tokenB, sessionB, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params":  map[string]any{"name": "watch_thread", "arguments": map[string]any{"thread_id": threadID}},
	}); watchResp["result"] == nil {
		t.Fatalf("watch_thread did not succeed: %v", watchResp)
	}
	subResp, _ := rawRPC(t, srv.URL, tokenB, sessionB, map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "resources/subscribe",
		"params":  map[string]any{"uri": "rendezvous://catchup/" + agentB.ID},
	})
	if _, ok := subResp["result"]; !ok {
		t.Fatalf("resources/subscribe did not succeed: %v", subResp)
	}

	events, cancel := openStandingStream(t, srv.URL, tokenB, sessionB)
	defer cancel()

	time.Sleep(2 * time.Second) // mirror the multi-minute real-world gap, scaled down

	replyResp, _ := rawRPC(t, srv.URL, tokenA, sessionA, map[string]any{
		"jsonrpc": "2.0",
		"id":      5,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "reply",
			"arguments": map[string]any{"thread_id": threadID, "body": "update, no mention"},
		},
	})
	if _, ok := replyResp["result"]; !ok {
		t.Fatalf("reply did not succeed: %v", replyResp)
	}

	select {
	case ev := <-events:
		t.Logf("received on standing GET stream: %s", ev)
	case <-time.After(5 * time.Second):
		t.Fatal("no notification arrived for the watcher-path reply, even after a realistic delay before firing")
	}
}

// TestPushNotification_DelayedPushSurvivesServerWriteTimeout closes the
// gap that let board bug 4f93edd4 stay dark: every other test in this
// file runs against httptest.NewServer, which builds an *http.Server with
// ZERO timeouts, while production (cmd/rendezvous/main.go newServer) sets
// WriteTimeout. Go resets that deadline only when a new request's headers
// are read, so on a standing GET it is a hard cap measured from when the
// stream opened — not an idle timeout. A push fired after it expires is
// never delivered and the connection is torn down, while the write
// reports success to the application: Flush() returns no error, so
// neither this app nor the SDK can see the failure. Server-side silence,
// client-side a stream that was open and simply never delivered anything.
//
// The WriteTimeout here is scaled down (500ms, trigger at +1.2s) to keep
// the test fast while preserving the "trigger fires after the deadline"
// relationship that every live round in 4f93edd4 had.
func TestPushNotification_DelayedPushSurvivesServerWriteTimeout(t *testing.T) {
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
	tokenA, err := auth.Issue(db, agentA.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken(agent-a) error = %v", err)
	}
	tokenB, err := auth.Issue(db, agentB.ID)
	if err != nil {
		t.Fatalf("IssueAgentToken(agent-b) error = %v", err)
	}

	const writeTimeout = 500 * time.Millisecond
	srv := newRawTestServer(t, mcpserver.Handler(db, []mcpserver.ToolRegistrar{rendezvous.NewRegistrar(db)}), func(s *http.Server) {
		s.WriteTimeout = writeTimeout
	})

	sessionB := initializeRawSession(t, srv.URL, tokenB)
	subResp, _ := rawRPC(t, srv.URL, tokenB, sessionB, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "resources/subscribe",
		"params":  map[string]any{"uri": "rendezvous://catchup/" + agentB.ID},
	})
	if _, ok := subResp["result"]; !ok {
		t.Fatalf("resources/subscribe did not succeed: %v", subResp)
	}

	events, cancel := openStandingStream(t, srv.URL, tokenB, sessionB)
	defer cancel()

	// Let the server's write deadline for this response expire before
	// anything is pushed, the way it always had in production.
	time.Sleep(writeTimeout + 700*time.Millisecond)

	// A second actor must fire the trigger: self-mentions write no row at
	// insert, so mentioning yourself produces a clean-looking negative
	// whether or not delivery works.
	sessionA := initializeRawSession(t, srv.URL, tokenA)
	callResp, _ := rawRPC(t, srv.URL, tokenA, sessionA, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "create_thread",
			"arguments": map[string]any{
				"title": "Delayed push repro",
				"body":  "cc @agent-b",
			},
		},
	})
	if _, ok := callResp["result"]; !ok {
		t.Fatalf("create_thread call did not succeed: %v", callResp)
	}

	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("standing GET stream was closed instead of receiving the push: the server's WriteTimeout is still capping this response")
		}
		if !strings.Contains(ev, "notifications/resources/updated") {
			t.Fatalf("unexpected event on standing GET stream: %s", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no notification arrived on a standing GET stream held past the server's WriteTimeout")
	}
}

func extractThreadID(t *testing.T, callResp map[string]any) string {
	t.Helper()
	result, ok := callResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call response missing result: %v", callResp)
	}
	content, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call result missing structuredContent: %v", result)
	}
	threadID, _ := content["thread_id"].(string)
	if threadID == "" {
		t.Fatalf("structuredContent missing thread_id: %v", content)
	}
	return threadID
}
