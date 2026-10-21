package secrethandoff

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"
)

type party struct {
	name     string
	encPub   []byte
	encPriv  []byte
	signPub  ed25519.PublicKey
	signPriv ed25519.PrivateKey
	bundle   *KeyBundle
}

func newParty(t *testing.T, name string, operator ed25519.PrivateKey) *party {
	t.Helper()
	encPub, encPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signPub, signPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b := &KeyBundle{
		Actor: name, EncPubKey: encPub[:], SignPubKey: signPub,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	b.OperatorSig = ed25519.Sign(operator, CanonicalBundleBytes(b))
	return &party{name, encPub[:], encPriv[:], signPub, signPriv, b}
}

func testOperator(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestSealOpenRoundTrip(t *testing.T) {
	opPub, opPriv := testOperator(t)
	alice := newParty(t, "alice", opPriv)
	bob := newParty(t, "bob", opPriv)

	if err := VerifyBundle(bob.bundle, opPub); err != nil {
		t.Fatalf("VerifyBundle(bob): %v", err)
	}

	env, err := Seal([]byte("the-credential"), "sid-1", "alice", "bob", "test-label",
		ClassRoutine, 15*time.Minute, bob.encPub, alice.signPriv)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	got, err := Open(env, "bob", alice.bundle, bob.encPub, bob.encPriv, time.Now())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(got) != "the-credential" {
		t.Fatalf("plaintext = %q", got)
	}
}

// The acceptance matrix from thread a6dc0141 — every case must fail closed.

func TestOpenRefusesWrongRecipient(t *testing.T) {
	_, opPriv := testOperator(t)
	alice := newParty(t, "alice", opPriv)
	bob := newParty(t, "bob", opPriv)
	carol := newParty(t, "carol", opPriv)

	env, _ := Seal([]byte("x"), "sid", "alice", "bob", "l", ClassRoutine, time.Minute, bob.encPub, alice.signPriv)
	if _, err := Open(env, "carol", alice.bundle, carol.encPub, carol.encPriv, time.Now()); err == nil {
		t.Fatal("Open accepted an envelope addressed to someone else")
	}
}

func TestOpenRefusesExpiredTTL(t *testing.T) {
	_, opPriv := testOperator(t)
	alice := newParty(t, "alice", opPriv)
	bob := newParty(t, "bob", opPriv)

	env, _ := Seal([]byte("x"), "sid", "alice", "bob", "l", ClassRoutine, time.Minute, bob.encPub, alice.signPriv)
	late := time.Now().Add(2 * time.Minute)
	if _, err := Open(env, "bob", alice.bundle, bob.encPub, bob.encPriv, late); err == nil {
		t.Fatal("Open accepted an expired envelope")
	}
}

func TestOpenRefusesForgedSender(t *testing.T) {
	_, opPriv := testOperator(t)
	alice := newParty(t, "alice", opPriv)
	bob := newParty(t, "bob", opPriv)
	mallory := newParty(t, "mallory", opPriv)

	// Mallory seals an envelope claiming From=alice but signs with her key.
	env, _ := Seal([]byte("x"), "sid", "alice", "bob", "l", ClassRoutine, time.Minute, bob.encPub, mallory.signPriv)
	if _, err := Open(env, "bob", alice.bundle, bob.encPub, bob.encPriv, time.Now()); err == nil {
		t.Fatal("Open accepted a sender signature that doesn't verify against the claimed sender's pinned bundle")
	}
}

func TestOpenRefusesTamperedHeader(t *testing.T) {
	_, opPriv := testOperator(t)
	alice := newParty(t, "alice", opPriv)
	bob := newParty(t, "bob", opPriv)
	carol := newParty(t, "carol", opPriv)

	env, _ := Seal([]byte("x"), "sid", "alice", "bob", "l", ClassRoutine, time.Minute, bob.encPub, alice.signPriv)
	env.Header.To = "carol" // redirect attempt after signing
	if _, err := Open(env, "carol", alice.bundle, carol.encPub, carol.encPriv, time.Now()); err == nil {
		t.Fatal("Open accepted a redirected (tampered) header")
	}
}

func TestOpenRefusesSwappedCiphertext(t *testing.T) {
	_, opPriv := testOperator(t)
	alice := newParty(t, "alice", opPriv)
	bob := newParty(t, "bob", opPriv)

	env1, _ := Seal([]byte("real"), "sid-1", "alice", "bob", "l", ClassRoutine, time.Minute, bob.encPub, alice.signPriv)
	env2, _ := Seal([]byte("fake"), "sid-2", "alice", "bob", "l", ClassRoutine, time.Minute, bob.encPub, alice.signPriv)
	env1.Ciphertext = env2.Ciphertext
	if _, err := Open(env1, "bob", alice.bundle, bob.encPub, bob.encPriv, time.Now()); err == nil {
		t.Fatal("Open accepted an envelope whose ciphertext doesn't match the signed hash")
	}
}

func TestVerifyBundleRefusesUnsignedAndForeignKey(t *testing.T) {
	opPub, opPriv := testOperator(t)
	_, rogura := testOperator(t) // a different "operator"
	alice := newParty(t, "alice", opPriv)

	unsigned := *alice.bundle
	unsigned.OperatorSig = nil
	if err := VerifyBundle(&unsigned, opPub); err == nil {
		t.Fatal("VerifyBundle accepted a bundle with no operator signature")
	}

	forged := *alice.bundle
	forged.OperatorSig = ed25519.Sign(rogura, CanonicalBundleBytes(&forged))
	if err := VerifyBundle(&forged, opPub); err == nil {
		t.Fatal("VerifyBundle accepted a bundle signed by a different key")
	}
}

func TestBundleFieldChangeInvalidatesSignature(t *testing.T) {
	opPub, opPriv := testOperator(t)
	alice := newParty(t, "alice", opPriv)
	mallory := newParty(t, "mallory", opPriv)

	// A stolen token republishes alice's bundle with mallory's enc key.
	swapped := *alice.bundle
	swapped.EncPubKey = mallory.encPub
	if err := VerifyBundle(&swapped, opPub); err == nil {
		t.Fatal("VerifyBundle accepted a bundle whose key was swapped after signing")
	}
}
