package mcpserver_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/crypto/nacl/box"

	"mcp-symposium/internal/secrethandoff"
	"mcp-symposium/internal/tools/rendezvous"
)

// callToolExpectError calls a tool and returns the error text it produced;
// it fails the test if the call unexpectedly succeeded.
func callToolExpectError(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error()
	}
	if !result.IsError {
		t.Fatalf("CallTool(%s) succeeded, want an error", name)
	}
	var sb strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// TestSecretHandoffEndToEnd drives the v2 flow over MCP: publish bundles,
// seal, send, fetch once, decrypt — then the server-side half of the
// acceptance matrix from thread a6dc0141 (one-time, foreign fetch, admin
// gate, body guard).
func TestSecretHandoffEndToEnd(t *testing.T) {
	db := openTestDB(t)
	_, senderToken := newAgentWithToken(t, db, "hand-sender")
	_, recipientToken := newAgentWithToken(t, db, "hand-recipient")
	_, thiefToken := newAgentWithToken(t, db, "hand-thief")

	senderSession, cleanupS := newTestSession(t, db, senderToken)
	defer cleanupS()
	recipientSession, cleanupR := newTestSession(t, db, recipientToken)
	defer cleanupR()
	thiefSession, cleanupT := newTestSession(t, db, thiefToken)
	defer cleanupT()

	// Operator + both parties' keys (what the board-secret CLI does locally).
	_, opPriv, _ := ed25519.GenerateKey(rand.Reader)
	sSignPub, sSignPriv, _ := ed25519.GenerateKey(rand.Reader)
	sEncPub, _, _ := box.GenerateKey(rand.Reader)
	rEncPub, rEncPriv, _ := box.GenerateKey(rand.Reader)
	rSignPub, _, _ := ed25519.GenerateKey(rand.Reader)

	mkBundle := func(actor string, encPub []byte, signPub ed25519.PublicKey) *secrethandoff.KeyBundle {
		b := &secrethandoff.KeyBundle{
			Actor: actor, EncPubKey: encPub, SignPubKey: signPub,
			CreatedAt: time.Now().UTC().Truncate(time.Second),
		}
		b.OperatorSig = ed25519.Sign(opPriv, secrethandoff.CanonicalBundleBytes(b))
		return b
	}
	senderBundle := mkBundle("hand-sender", sEncPub[:], sSignPub)
	recipientBundle := mkBundle("hand-recipient", rEncPub[:], rSignPub)

	// Publish both bundles over MCP.
	sbJSON, _ := json.Marshal(senderBundle)
	rbJSON, _ := json.Marshal(recipientBundle)
	var setOut rendezvous.SetKeyBundleOutput
	callTool(t, senderSession, "set_key_bundle", map[string]any{"bundle_json": string(sbJSON)}, &setOut)
	callTool(t, recipientSession, "set_key_bundle", map[string]any{"bundle_json": string(rbJSON)}, &setOut)

	// The sender fetches the recipient's bundle and seals to it.
	var getOut rendezvous.GetKeyBundleOutput
	callTool(t, senderSession, "get_key_bundle", map[string]any{"actor": "hand-recipient"}, &getOut)
	var fetchedBundle secrethandoff.KeyBundle
	if err := json.Unmarshal([]byte(getOut.BundleJSON), &fetchedBundle); err != nil {
		t.Fatalf("unmarshal fetched bundle: %v", err)
	}

	env, err := secrethandoff.Seal([]byte("sk-live-credential"), "", "hand-sender", "hand-recipient",
		"test-cred", secrethandoff.ClassRoutine, 10*time.Minute, fetchedBundle.EncPubKey, sSignPriv)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	envJSON, _ := secrethandoff.MarshalEnvelope(env)

	var sendOut rendezvous.SendSecretOutput
	callTool(t, senderSession, "send_secret", map[string]any{
		"to": "hand-recipient", "envelope_json": string(envJSON),
	}, &sendOut)
	if sendOut.SecretID == "" {
		t.Fatal("send_secret returned no id")
	}

	// A third actor's fetch is refused and counted.
	msg := callToolExpectError(t, thiefSession, "fetch_secret", map[string]any{"secret_id": sendOut.SecretID})
	if !strings.Contains(msg, "not addressed to you") {
		t.Fatalf("foreign fetch error = %q", msg)
	}

	// The recipient fetches, decrypts, and the plaintext round-trips.
	var fetchOut rendezvous.FetchSecretOutput
	callTool(t, recipientSession, "fetch_secret", map[string]any{"secret_id": sendOut.SecretID}, &fetchOut)
	gotEnv, err := secrethandoff.UnmarshalEnvelope([]byte(fetchOut.EnvelopeJSON))
	if err != nil {
		t.Fatalf("unmarshal fetched envelope: %v", err)
	}
	plaintext, err := secrethandoff.Open(gotEnv, "hand-recipient", senderBundle, rEncPub[:], rEncPriv[:], time.Now())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(plaintext) != "sk-live-credential" {
		t.Fatalf("plaintext = %q", plaintext)
	}

	// Second fetch: the theft alarm.
	msg = callToolExpectError(t, recipientSession, "fetch_secret", map[string]any{"secret_id": sendOut.SecretID})
	if !strings.Contains(msg, "already fetched") {
		t.Fatalf("double fetch error = %q", msg)
	}
}

// TestAdminClassSecretNeedsApproval covers the operator gate over MCP.
func TestAdminClassSecretNeedsApproval(t *testing.T) {
	db := openTestDB(t)
	_, senderToken := newAgentWithToken(t, db, "adm-sender")
	_, recipientToken := newAgentWithToken(t, db, "adm-recipient")

	senderSession, cleanupS := newTestSession(t, db, senderToken)
	defer cleanupS()
	recipientSession, cleanupR := newTestSession(t, db, recipientToken)
	defer cleanupR()

	_, opPriv, _ := ed25519.GenerateKey(rand.Reader)
	_, sSignPriv, _ := ed25519.GenerateKey(rand.Reader)
	rEncPub, _, _ := box.GenerateKey(rand.Reader)
	_ = opPriv

	env, err := secrethandoff.Seal([]byte("cluster-admin-cred"), "", "adm-sender", "adm-recipient",
		"k8s-admin", secrethandoff.ClassAdmin, 10*time.Minute, rEncPub[:], sSignPriv)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	envJSON, _ := secrethandoff.MarshalEnvelope(env)

	var sendOut rendezvous.SendSecretOutput
	callTool(t, senderSession, "send_secret", map[string]any{
		"to": "adm-recipient", "envelope_json": string(envJSON),
	}, &sendOut)

	msg := callToolExpectError(t, recipientSession, "fetch_secret", map[string]any{"secret_id": sendOut.SecretID})
	if !strings.Contains(msg, "operator approval") {
		t.Fatalf("admin fetch error = %q, want the approval gate", msg)
	}
}

// TestBodyGuardRejectsSecretShapedPosts covers the defence-in-depth guard:
// a post with a credential-shaped string is refused before it is stored.
func TestBodyGuardRejectsSecretShapedPosts(t *testing.T) {
	db := openTestDB(t)
	_, token := newAgentWithToken(t, db, "guard-agent")
	session, cleanup := newTestSession(t, db, token)
	defer cleanup()

	msg := callToolExpectError(t, session, "create_thread", map[string]any{
		"title": "deploy notes",
		"body":  "the new key is sk-abcdefghijklmnop1234 please use it",
	})
	if !strings.Contains(msg, "secret-shaped") {
		t.Fatalf("create_thread error = %q, want the secret guard", msg)
	}

	// A clean post still works, and replying to it with a PEM block fails.
	var created rendezvous.CreateThreadOutput
	callTool(t, session, "create_thread", map[string]any{
		"title": "deploy notes", "body": "key rotation done, see secret handoff",
	}, &created)

	msg = callToolExpectError(t, session, "reply", map[string]any{
		"thread_id": created.ThreadID,
		"body":      "-----BEGIN RSA PRIVATE KEY-----\nMIIE...",
	})
	if !strings.Contains(msg, "secret-shaped") {
		t.Fatalf("reply error = %q, want the secret guard", msg)
	}
}

// TestSendSecretRefusesForgedFrom pins the k8s negative-pass finding 1
// (server half): an enrolled caller cannot send an envelope whose header
// claims a different From.
func TestSendSecretRefusesForgedFrom(t *testing.T) {
	db := openTestDB(t)
	_, impToken := newAgentWithToken(t, db, "imp-c")
	newAgentWithToken(t, db, "imp-a") // the victim whose name is claimed
	_, recipToken := newAgentWithToken(t, db, "imp-r")
	_ = recipToken

	impSession, cleanup := newTestSession(t, db, impToken)
	defer cleanup()

	_, signPriv, _ := ed25519.GenerateKey(rand.Reader)
	rEncPub, _, _ := box.GenerateKey(rand.Reader)
	env, err := secrethandoff.Seal([]byte("x"), "", "imp-a", "imp-r", "l",
		secrethandoff.ClassRoutine, time.Minute, rEncPub[:], signPriv)
	if err != nil {
		t.Fatal(err)
	}
	envJSON, _ := secrethandoff.MarshalEnvelope(env)

	msg := callToolExpectError(t, impSession, "send_secret", map[string]any{
		"to": "imp-r", "envelope_json": string(envJSON),
	})
	if !strings.Contains(msg, "not the calling actor") {
		t.Fatalf("forged From error = %q", msg)
	}
}

// TestSetKeyBundleRefusesForeignActor pins the k8s finding 2: a caller
// cannot publish a bundle naming another actor.
func TestSetKeyBundleRefusesForeignActor(t *testing.T) {
	db := openTestDB(t)
	_, cToken := newAgentWithToken(t, db, "bundle-c")
	newAgentWithToken(t, db, "bundle-a")

	session, cleanup := newTestSession(t, db, cToken)
	defer cleanup()

	_, opPriv, _ := ed25519.GenerateKey(rand.Reader)
	encPub, _, _ := box.GenerateKey(rand.Reader)
	signPub, _, _ := ed25519.GenerateKey(rand.Reader)
	b := &secrethandoff.KeyBundle{
		Actor: "bundle-a", EncPubKey: encPub[:], SignPubKey: signPub,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	b.OperatorSig = ed25519.Sign(opPriv, secrethandoff.CanonicalBundleBytes(b))
	bj, _ := json.Marshal(b)

	msg := callToolExpectError(t, session, "set_key_bundle", map[string]any{"bundle_json": string(bj)})
	if !strings.Contains(msg, "not the calling actor") {
		t.Fatalf("foreign bundle error = %q", msg)
	}
}

// TestToolAllowlistScopesActor pins the per-actor tool allowlist: a scoped
// actor can call what it's allowed and nothing else.
func TestToolAllowlistScopesActor(t *testing.T) {
	db := openTestDB(t)
	scoped, token := newAgentWithToken(t, db, "scoped-poster")
	if err := db.Model(scoped).Update("tool_allowlist", "create_thread,reply,get_thread,list_threads").Error; err != nil {
		t.Fatal(err)
	}

	session, cleanup := newTestSession(t, db, token)
	defer cleanup()

	var created rendezvous.CreateThreadOutput
	callTool(t, session, "create_thread", map[string]any{
		"title": "allowed", "body": "scoped actor can post",
	}, &created)

	msg := callToolExpectError(t, session, "catch_up", map[string]any{})
	if !strings.Contains(msg, "scoped token") {
		t.Fatalf("catch_up as scoped actor = %q, want scoped-token refusal", msg)
	}
	msg = callToolExpectError(t, session, "fetch_secret", map[string]any{"secret_id": "any"})
	if !strings.Contains(msg, "scoped token") {
		t.Fatalf("fetch_secret as scoped actor = %q, want scoped-token refusal", msg)
	}
}

// TestExternalIDUpsert pins the dedup contract: same external_id returns
// the existing thread instead of a duplicate, and get_thread resolves it.
func TestExternalIDUpsert(t *testing.T) {
	db := openTestDB(t)
	_, token := newAgentWithToken(t, db, "sentinel")
	session, cleanup := newTestSession(t, db, token)
	defer cleanup()

	var first rendezvous.CreateThreadOutput
	callTool(t, session, "create_thread", map[string]any{
		"title": "finding fp-123", "body": "first occurrence", "external_id": "fp-123",
	}, &first)
	if first.Existing {
		t.Fatal("first create reported existing")
	}

	var second rendezvous.CreateThreadOutput
	callTool(t, session, "create_thread", map[string]any{
		"title": "finding fp-123", "body": "repeat", "external_id": "fp-123",
	}, &second)
	if !second.Existing || second.ThreadID != first.ThreadID {
		t.Fatalf("second create = %+v, want existing thread %s", second, first.ThreadID)
	}

	var got rendezvous.GetThreadOutput
	callTool(t, session, "get_thread", map[string]any{"external_id": "fp-123"}, &got)
	if got.Thread.ID != first.ThreadID {
		t.Fatalf("get_thread by external_id = %s, want %s", got.Thread.ID, first.ThreadID)
	}
}
