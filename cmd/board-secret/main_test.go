package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"

	"mcp-symposium/internal/secrethandoff"
)

// testHome points HOME at a temp dir so keysDir()/seenPath()/the jail are
// isolated per test.
func testHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func writeAgentKeys(t *testing.T, actor string, operatorPub ed25519.PublicKey) *agentKeys {
	t.Helper()
	encPub, encPriv, _ := box.GenerateKey(rand.Reader)
	signPub, signPriv, _ := ed25519.GenerateKey(rand.Reader)
	k := &agentKeys{
		Actor: actor, EncPub: encPub[:], EncPriv: encPriv[:],
		SignPub: signPub, SignPriv: signPriv,
		OperatorPub: operatorPub, CreatedAt: time.Now().Unix(),
	}
	if err := saveKeys(k); err != nil {
		t.Fatalf("saveKeys: %v", err)
	}
	return k
}

func signedBundle(t *testing.T, actor string, encPub []byte, signPub ed25519.PublicKey,
	opPriv ed25519.PrivateKey, createdAt time.Time) *secrethandoff.KeyBundle {
	t.Helper()
	b := &secrethandoff.KeyBundle{
		Actor: actor, EncPubKey: encPub, SignPubKey: signPub,
		CreatedAt: createdAt.UTC().Truncate(time.Second),
	}
	b.OperatorSig = ed25519.Sign(opPriv, secrethandoff.CanonicalBundleBytes(b))
	return b
}

func writeBundleFile(t *testing.T, dir string, b *secrethandoff.KeyBundle) string {
	t.Helper()
	data, _ := json.Marshal(b)
	p := filepath.Join(dir, b.Actor+".json")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConfineToSecretsDir(t *testing.T) {
	home := testHome(t)
	jail := filepath.Join(home, ".secrets")
	if err := os.MkdirAll(jail, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := confineToSecretsDir("../outside"); err == nil {
		t.Error("accepted a .. escape")
	}
	if _, err := confineToSecretsDir("/tmp/elsewhere"); err == nil {
		t.Error("accepted an absolute path outside the jail")
	}
	if p, err := confineToSecretsDir("token.txt"); err != nil || p != filepath.Join(jail, "token.txt") {
		t.Errorf("plain name: p=%q err=%v", p, err)
	}
}

// TestConfineRefusesSymlinkedJail pins the hole the k8s negative pass
// found: ~/.secrets itself being a symlink moved the write outside.
func TestConfineRefusesSymlinkedJail(t *testing.T) {
	home := testHome(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, ".secrets")); err != nil {
		t.Fatal(err)
	}
	if _, err := confineToSecretsDir("x"); err == nil {
		t.Fatal("accepted a symlinked ~/.secrets")
	}
}

// TestOpenVerifiedSenderMustMatchHeaderFrom pins vllm's review item 2 /
// k8s's finding 1: an enrolled actor sealing with its own key but claiming
// another From must be refused by the CLI.
func TestOpenVerifiedSenderMustMatchHeaderFrom(t *testing.T) {
	home := testHome(t)
	opPub, opPriv, _ := ed25519.GenerateKey(rand.Reader)
	me := writeAgentKeys(t, "me", opPub)

	// Mallory is enrolled (operator-signed) but claims From=alice.
	mEncPub, _, _ := box.GenerateKey(rand.Reader)
	mSignPub, mSignPriv, _ := ed25519.GenerateKey(rand.Reader)
	malloryBundle := signedBundle(t, "mallory", mEncPub[:], mSignPub, opPriv, time.Now())
	bundlePath := writeBundleFile(t, home, malloryBundle)

	env, err := secrethandoff.Seal([]byte("x"), "sid-imp", "alice", "me", "l",
		secrethandoff.ClassRoutine, time.Minute, me.EncPub, mSignPriv)
	if err != nil {
		t.Fatal(err)
	}
	envJSON, _ := secrethandoff.MarshalEnvelope(env)

	if err := runWithStdin(t, string(envJSON), "open", "--from-bundle", bundlePath, "--out", "x"); err == nil {
		t.Fatal("open accepted an envelope whose From doesn't match the bundle's actor")
	} else if !strings.Contains(err.Error(), "claims From") {
		t.Fatalf("wrong refusal: %v", err)
	}
}

// TestOpenRefusesReplayBeforeWriting pins vllm's item 3: the seen-list is
// written (fsynced) before the secret, and a second open of the same id
// fails even though the first one completed.
func TestOpenReplayRefused(t *testing.T) {
	home := testHome(t)
	opPub, opPriv, _ := ed25519.GenerateKey(rand.Reader)
	me := writeAgentKeys(t, "me", opPub)

	aEncPub, _, _ := box.GenerateKey(rand.Reader)
	aSignPub, aSignPriv, _ := ed25519.GenerateKey(rand.Reader)
	aliceBundle := signedBundle(t, "alice", aEncPub[:], aSignPub, opPriv, time.Now())
	bundlePath := writeBundleFile(t, home, aliceBundle)

	env, _ := secrethandoff.Seal([]byte("v"), "sid-replay", "alice", "me", "l",
		secrethandoff.ClassRoutine, time.Minute, me.EncPub, aSignPriv)
	envJSON, _ := secrethandoff.MarshalEnvelope(env)

	if err := runWithStdin(t, string(envJSON), "open", "--from-bundle", bundlePath, "--out", "a"); err != nil {
		t.Fatalf("first open: %v", err)
	}
	err := runWithStdin(t, string(envJSON), "open", "--from-bundle", bundlePath, "--out", "b", "--force")
	if err == nil || !strings.Contains(err.Error(), "replay") {
		t.Fatalf("replayed open err = %v, want replay refusal", err)
	}
}

// TestOpenForceResetsPermissions pins k8s finding 4: overwriting a 0644
// file must leave 0600.
func TestOpenForceResetsPermissions(t *testing.T) {
	home := testHome(t)
	opPub, opPriv, _ := ed25519.GenerateKey(rand.Reader)
	me := writeAgentKeys(t, "me", opPub)

	aEncPub, _, _ := box.GenerateKey(rand.Reader)
	aSignPub, aSignPriv, _ := ed25519.GenerateKey(rand.Reader)
	bundlePath := writeBundleFile(t, home, signedBundle(t, "alice", aEncPub[:], aSignPub, opPriv, time.Now()))

	jail := filepath.Join(home, ".secrets")
	if err := os.MkdirAll(jail, 0o700); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(jail, "t")
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	env, _ := secrethandoff.Seal([]byte("new"), "sid-perm", "alice", "me", "l",
		secrethandoff.ClassRoutine, time.Minute, me.EncPub, aSignPriv)
	envJSON, _ := secrethandoff.MarshalEnvelope(env)
	if err := runWithStdin(t, string(envJSON), "open", "--from-bundle", bundlePath, "--out", "t", "--force"); err != nil {
		t.Fatalf("open --force: %v", err)
	}
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", fi.Mode().Perm())
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "new" {
		t.Fatalf("content = %q", data)
	}
}

// TestBundleRollbackRefused pins vllm's item 1: once a newer bundle for an
// actor has been accepted on this host, an older (rotated-away) one is
// refused even though its operator signature verifies.
func TestBundleRollbackRefused(t *testing.T) {
	home := testHome(t)
	opPub, opPriv, _ := ed25519.GenerateKey(rand.Reader)
	k := writeAgentKeys(t, "me", opPub)

	aSignPub, _, _ := ed25519.GenerateKey(rand.Reader)
	oldEnc, _, _ := box.GenerateKey(rand.Reader)
	newEnc, _, _ := box.GenerateKey(rand.Reader)
	oldBundle := signedBundle(t, "alice", oldEnc[:], aSignPub, opPriv, time.Now().Add(-time.Hour))
	newBundle := signedBundle(t, "alice", newEnc[:], aSignPub, opPriv, time.Now())

	newPath := writeBundleFile(t, filepath.Join(home), newBundle)
	if _, err := loadVerifiedBundle(newPath, k); err != nil {
		t.Fatalf("new bundle: %v", err)
	}
	oldDir := t.TempDir()
	oldPath := writeBundleFile(t, oldDir, oldBundle)
	if _, err := loadVerifiedBundle(oldPath, k); err == nil {
		t.Fatal("accepted an older bundle after a newer one was pinned (rollback)")
	}
}

// runWithStdin runs the CLI's run() with stdin replaced by input.
func runWithStdin(t *testing.T, input string, args ...string) error {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	go func() {
		w.WriteString(input)
		w.Close()
	}()
	return run(args)
}

// TestOperatorKeyEncryptionRoundTrip pins the trust-root protection: the
// private half on disk is passphrase-encrypted, a wrong passphrase fails,
// and the legacy plaintext format is refused outright.
func TestOperatorKeyEncryptionRoundTrip(t *testing.T) {
	home := testHome(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	enc, err := sealOperatorKey(pub, priv, []byte("correct horse"))
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(enc)
	path := filepath.Join(home, "operator.key")
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}

	gotPub, gotPriv, err := openOperatorKey(path, []byte("correct horse"))
	if err != nil {
		t.Fatalf("open with right passphrase: %v", err)
	}
	if !gotPub.Equal(pub) || !gotPriv.Equal(priv) {
		t.Fatal("keys did not round-trip")
	}

	if _, _, err := openOperatorKey(path, []byte("wrong")); err == nil {
		t.Fatal("opened with the wrong passphrase")
	}

	// Legacy plaintext format must be refused, not silently used.
	legacy, _ := json.Marshal(map[string][]byte{"pub": pub, "priv": priv})
	legacyPath := filepath.Join(home, "legacy.key")
	if err := os.WriteFile(legacyPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = openOperatorKey(legacyPath, []byte("whatever"))
	if err == nil || !strings.Contains(err.Error(), "UNENCRYPTED") {
		t.Fatalf("legacy key err = %v, want the unencrypted-format refusal", err)
	}
}
