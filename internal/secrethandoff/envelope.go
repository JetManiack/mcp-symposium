// Package secrethandoff implements the end-to-end encrypted credential
// handoff agreed in board thread a6dc0141 (v2 spec): operator-signed key
// bundles as the trust root, sealed+signed envelopes, one-time fetch with
// TTL. The server stays crypto-blind — it stores bundles and envelopes as
// opaque records and never holds a key; everything in this package is for
// the CLI and for tests, except the envelope field validation the server
// does on metadata.
//
// Crypto: X25519 sealed box (golang.org/x/crypto/nacl/box, ephemeral
// sender key per envelope, so a later leak of a static key does not open
// past blobs) for confidentiality, plus Ed25519 over the envelope header
// for sender authenticity. Wire-compatible with libsodium
// crypto_box_seal / crypto_sign_detached.
package secrethandoff

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/nacl/box"
)

// MaxTTL caps how long an unsent-for secret survives on the server.
const MaxTTL = time.Hour

// Class labels what kind of credential an envelope carries. Routine
// secrets release on fetch; admin secrets stay blocked until the operator
// approves that secret_id.
const (
	ClassRoutine = "routine"
	ClassAdmin   = "admin"
)

// KeyBundle is an agent's published identity: its encryption and signing
// public keys, signed by the operator key. The signature is over
// CanonicalBundleBytes, so neither the server nor a stolen agent token can
// alter any field without invalidating it.
type KeyBundle struct {
	Actor string `json:"actor"`
	// ActorID is the immutable actor UUID on the board. Names (nicknames,
	// display names) are mutable and can be grabbed by a stolen token; the
	// ID is assigned by the server at actor creation and never changes, so
	// it — not the name — is what the operator signature binds the keys to.
	ActorID    string    `json:"actor_id"`
	EncPubKey  []byte    `json:"enc_pubkey"`  // X25519, 32 bytes
	SignPubKey []byte    `json:"sign_pubkey"` // Ed25519, 32 bytes
	CreatedAt  time.Time `json:"created_at"`
	// OperatorSig is ed25519.Sign(operatorKey, CanonicalBundleBytes(bundle)).
	OperatorSig []byte `json:"operator_sig"`
}

// CanonicalBundleBytes is the byte string the operator signs: a fixed
// field order, independent of JSON map ordering.
func CanonicalBundleBytes(b *KeyBundle) []byte {
	return fmt.Appendf(nil, "symposium-key-bundle-v2\x00%s\x00%s\x00%s\x00%s\x00%d",
		b.Actor,
		b.ActorID,
		base64.StdEncoding.EncodeToString(b.EncPubKey),
		base64.StdEncoding.EncodeToString(b.SignPubKey),
		b.CreatedAt.UTC().Unix())
}

// VerifyBundle checks the operator signature and key sizes.
func VerifyBundle(b *KeyBundle, operatorPub ed25519.PublicKey) error {
	if b.ActorID == "" {
		return errors.New("bundle has no actor_id (pre-v2 format) — regenerate and re-sign it")
	}
	if len(b.EncPubKey) != 32 {
		return fmt.Errorf("enc_pubkey must be 32 bytes, got %d", len(b.EncPubKey))
	}
	if len(b.SignPubKey) != ed25519.PublicKeySize {
		return fmt.Errorf("sign_pubkey must be %d bytes, got %d", ed25519.PublicKeySize, len(b.SignPubKey))
	}
	if !ed25519.Verify(operatorPub, CanonicalBundleBytes(b), b.OperatorSig) {
		return errors.New("bundle is not signed by the operator key")
	}
	return nil
}

// Fingerprint renders the value the operator compares between the agent
// host's terminal and the signer: sha256 over the canonical bundle bytes
// minus the signature.
func Fingerprint(b *KeyBundle) string {
	sum := sha256.Sum256(CanonicalBundleBytes(b))
	return hex.EncodeToString(sum[:8]) // 16 hex chars: short enough to compare by eye
}

// EnvelopeHeader is the signed, serverside-visible part of a handoff.
// The sender's Ed25519 signature covers CanonicalHeaderBytes, which binds
// recipient, label, class, time, TTL and the ciphertext hash — so an
// envelope can't be replayed to another recipient, relabeled, reclassed,
// or have its payload swapped.
type EnvelopeHeader struct {
	V        int    `json:"v"` // format version, 2
	SecretID string `json:"secret_id"`
	From     string `json:"from"` // sender actor nickname (display)
	To       string `json:"to"`   // recipient actor nickname (display)
	// FromID/ToID are the immutable actor UUIDs — what the server and the
	// CLI actually verify against. The names above are for humans reading
	// audit output; they carry no authority.
	FromID     string    `json:"from_id"`
	ToID       string    `json:"to_id"`
	Label      string    `json:"label"`
	Class      string    `json:"class"` // routine | admin
	CreatedAt  time.Time `json:"created_at"`
	TTLSeconds int       `json:"ttl_s"`
	// CiphertextSHA256 is hex(sha256(ciphertext)).
	CiphertextSHA256 string `json:"ciphertext_sha256"`
}

// Envelope is what travels through the server: the signed header, the
// sender's signature, and the sealed ciphertext. The server never reads
// Ciphertext beyond size checks.
type Envelope struct {
	Header    EnvelopeHeader `json:"header"`
	SenderSig []byte         `json:"sender_sig"`
	// Ciphertext is box.SealAnonymous(plaintext, recipientEncPub).
	Ciphertext []byte `json:"ciphertext"`
}

// CanonicalHeaderBytes is the byte string the sender signs.
func CanonicalHeaderBytes(h *EnvelopeHeader) []byte {
	return fmt.Appendf(nil, "symposium-secret-envelope-v%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%s",
		h.V, h.SecretID, h.From, h.FromID, h.To, h.ToID, h.Label, h.Class,
		h.CreatedAt.UTC().Unix(), h.TTLSeconds, h.CiphertextSHA256)
}

// Seal builds a complete envelope: seals plaintext to the recipient's
// X25519 key and signs the header with the sender's Ed25519 key.
func Seal(plaintext []byte, secretID, from, fromID string, recipient *KeyBundle,
	label, class string, ttl time.Duration,
	senderSignPriv ed25519.PrivateKey) (*Envelope, error) {
	// The recipient is the operator-signed bundle, not loose (name, key)
	// args: the name, the immutable id and the enc key all come from the
	// same signed object, so they can't be mixed and matched.
	if recipient == nil || len(recipient.EncPubKey) != 32 {
		return nil, fmt.Errorf("recipient bundle missing or enc_pubkey malformed")
	}
	if recipient.ActorID == "" || fromID == "" {
		return nil, fmt.Errorf("sender and recipient actor IDs are required (pre-v2 bundle?)")
	}
	if ttl <= 0 || ttl > MaxTTL {
		return nil, fmt.Errorf("ttl must be in (0, %s], got %s", MaxTTL, ttl)
	}
	if class != ClassRoutine && class != ClassAdmin {
		return nil, fmt.Errorf("class must be %q or %q", ClassRoutine, ClassAdmin)
	}

	var pub [32]byte
	copy(pub[:], recipient.EncPubKey)
	ciphertext, err := box.SealAnonymous(nil, plaintext, &pub, rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("seal: %w", err)
	}

	sum := sha256.Sum256(ciphertext)
	header := EnvelopeHeader{
		V:                2,
		SecretID:         secretID,
		From:             from,
		FromID:           fromID,
		To:               recipient.Actor,
		ToID:             recipient.ActorID,
		Label:            label,
		Class:            class,
		CreatedAt:        time.Now().UTC().Truncate(time.Second),
		TTLSeconds:       int(ttl.Seconds()),
		CiphertextSHA256: hex.EncodeToString(sum[:]),
	}
	sig := ed25519.Sign(senderSignPriv, CanonicalHeaderBytes(&header))
	return &Envelope{Header: header, SenderSig: sig, Ciphertext: ciphertext}, nil
}

// OpenErrors are distinguishable so the CLI can tell the user (and the
// operator) exactly which check refused the envelope.
var (
	ErrWrongRecipient = errors.New("envelope is addressed to a different actor")
	ErrExpired        = errors.New("envelope is outside its TTL")
	ErrBadSignature   = errors.New("sender signature does not verify against the pinned bundle")
	ErrTampered       = errors.New("ciphertext hash does not match the signed header")
)

// Open verifies and decrypts an envelope for recipient `me`. senderBundle
// must already be operator-verified by the caller (VerifyBundle) — Open
// checks the sender signature against it, the addressing, the TTL window
// and the ciphertext hash, then unseals.
func Open(env *Envelope, myActorID string, senderBundle *KeyBundle,
	myEncPub, myEncPriv []byte, now time.Time) ([]byte, error) {
	h := &env.Header
	if h.V != 2 {
		return nil, fmt.Errorf("unsupported envelope version %d (regenerate with a current CLI)", h.V)
	}
	// Addressing and sender identity are checked by immutable actor ID —
	// names are mutable and can be grabbed by a stolen token.
	if h.ToID != myActorID {
		return nil, fmt.Errorf("%w: to_id=%q, me=%q", ErrWrongRecipient, h.ToID, myActorID)
	}
	if senderBundle.ActorID == "" || h.FromID != senderBundle.ActorID {
		return nil, fmt.Errorf("%w: envelope from_id=%q but the sender bundle is for actor id %q", ErrBadSignature, h.FromID, senderBundle.ActorID)
	}
	if !ed25519.Verify(ed25519.PublicKey(senderBundle.SignPubKey), CanonicalHeaderBytes(h), env.SenderSig) {
		return nil, ErrBadSignature
	}
	// Range-check before the multiply (overflow) and enforce the cap on the
	// client too: a signed-but-oversized TTL must not extend the window.
	if h.TTLSeconds <= 0 || h.TTLSeconds > int(MaxTTL.Seconds()) {
		return nil, fmt.Errorf("%w: ttl_s %d outside (0, %d]", ErrExpired, h.TTLSeconds, int(MaxTTL.Seconds()))
	}
	expiry := h.CreatedAt.Add(time.Duration(h.TTLSeconds) * time.Second)
	if now.Before(h.CreatedAt.Add(-time.Minute)) || now.After(expiry) {
		return nil, fmt.Errorf("%w: created %s, ttl %ds, now %s", ErrExpired, h.CreatedAt, h.TTLSeconds, now)
	}
	sum := sha256.Sum256(env.Ciphertext)
	if hex.EncodeToString(sum[:]) != h.CiphertextSHA256 {
		return nil, ErrTampered
	}

	var pub, priv [32]byte
	copy(pub[:], myEncPub)
	copy(priv[:], myEncPriv)
	plaintext, ok := box.OpenAnonymous(nil, env.Ciphertext, &pub, &priv)
	if !ok {
		return nil, errors.New("decryption failed: wrong recipient key or corrupted ciphertext")
	}
	return plaintext, nil
}

// MarshalEnvelope / UnmarshalEnvelope are the single wire format (JSON,
// base64 []byte fields) shared by the tools and the CLI.
func MarshalEnvelope(e *Envelope) ([]byte, error) { return json.Marshal(e) }

func UnmarshalEnvelope(data []byte) (*Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	return &e, nil
}
