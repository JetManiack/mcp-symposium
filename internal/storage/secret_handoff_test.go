package storage_test

import (
	"errors"
	"testing"
	"time"

	"mcp-symposium/internal/storage"
)

func TestFetchSecretHandoff_OneTime(t *testing.T) {
	db := openTestDB(t)
	sender, _ := storage.CreateAgent(db, "sender")
	recipient, _ := storage.CreateAgent(db, "recipient")

	now := time.Now()
	h := &storage.SecretHandoff{
		ID: "sid-1", FromActorID: sender.ID, ToActorID: recipient.ID,
		Label: "l", Class: "routine", EnvelopeJSON: `{"ciphertext":"..."}`,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := storage.CreateSecretHandoff(db, h); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, env, err := storage.FetchSecretHandoff(db, "sid-1", recipient.ID, now)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if env != `{"ciphertext":"..."}` {
		t.Fatalf("envelope = %q", env)
	}

	// The blob is gone from the row.
	var raw storage.SecretHandoff
	if err := db.First(&raw, "id = ?", "sid-1").Error; err != nil {
		t.Fatal(err)
	}
	if raw.EnvelopeJSON != "" {
		t.Fatal("envelope blob survived the fetch")
	}

	// Second fetch by the recipient: the theft alarm.
	_, _, err = storage.FetchSecretHandoff(db, "sid-1", recipient.ID, now)
	if !errors.Is(err, storage.ErrSecretAlreadyFetched) {
		t.Fatalf("second fetch err = %v, want ErrSecretAlreadyFetched", err)
	}
}

func TestFetchSecretHandoff_ForeignFetchCountsAndRefuses(t *testing.T) {
	db := openTestDB(t)
	sender, _ := storage.CreateAgent(db, "sender")
	recipient, _ := storage.CreateAgent(db, "recipient")
	thief, _ := storage.CreateAgent(db, "thief")

	now := time.Now()
	h := &storage.SecretHandoff{
		ID: "sid-2", FromActorID: sender.ID, ToActorID: recipient.ID,
		Label: "l", Class: "routine", EnvelopeJSON: `{}`,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := storage.CreateSecretHandoff(db, h); err != nil {
		t.Fatal(err)
	}

	_, _, err := storage.FetchSecretHandoff(db, "sid-2", thief.ID, now)
	if !errors.Is(err, storage.ErrSecretForeignFetch) {
		t.Fatalf("foreign fetch err = %v, want ErrSecretForeignFetch", err)
	}

	var raw storage.SecretHandoff
	if err := db.First(&raw, "id = ?", "sid-2").Error; err != nil {
		t.Fatal(err)
	}
	if raw.ForeignAttempts != 1 {
		t.Fatalf("ForeignAttempts = %d, want 1", raw.ForeignAttempts)
	}
	if raw.EnvelopeJSON == "" {
		t.Fatal("foreign fetch must not consume the envelope")
	}

	// The real recipient can still fetch afterwards.
	if _, _, err := storage.FetchSecretHandoff(db, "sid-2", recipient.ID, now); err != nil {
		t.Fatalf("legitimate fetch after foreign attempt: %v", err)
	}
}

func TestFetchSecretHandoff_TTLAndAdminGate(t *testing.T) {
	db := openTestDB(t)
	sender, _ := storage.CreateAgent(db, "sender")
	recipient, _ := storage.CreateAgent(db, "recipient")

	now := time.Now()
	expired := &storage.SecretHandoff{
		ID: "sid-exp", FromActorID: sender.ID, ToActorID: recipient.ID,
		Class: "routine", EnvelopeJSON: `{}`, CreatedAt: now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(-time.Hour),
	}
	admin := &storage.SecretHandoff{
		ID: "sid-admin", FromActorID: sender.ID, ToActorID: recipient.ID,
		Class: "admin", EnvelopeJSON: `{}`, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	for _, h := range []*storage.SecretHandoff{expired, admin} {
		if err := storage.CreateSecretHandoff(db, h); err != nil {
			t.Fatal(err)
		}
	}

	if _, _, err := storage.FetchSecretHandoff(db, "sid-exp", recipient.ID, now); !errors.Is(err, storage.ErrSecretNotFound) {
		t.Fatalf("expired fetch err = %v, want ErrSecretNotFound", err)
	}

	if _, _, err := storage.FetchSecretHandoff(db, "sid-admin", recipient.ID, now); !errors.Is(err, storage.ErrSecretNeedsApproval) {
		t.Fatalf("unapproved admin fetch err = %v, want ErrSecretNeedsApproval", err)
	}

	if err := storage.ApproveSecretHandoff(db, "sid-admin", "test-operator", now); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, _, err := storage.FetchSecretHandoff(db, "sid-admin", recipient.ID, now); err != nil {
		t.Fatalf("approved admin fetch: %v", err)
	}
}

func TestSweepExpiredSecrets(t *testing.T) {
	db := openTestDB(t)
	sender, _ := storage.CreateAgent(db, "sender")
	recipient, _ := storage.CreateAgent(db, "recipient")

	now := time.Now()
	h := &storage.SecretHandoff{
		ID: "sid-sweep", FromActorID: sender.ID, ToActorID: recipient.ID,
		Class: "routine", EnvelopeJSON: `{"x":1}`,
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour),
	}
	if err := storage.CreateSecretHandoff(db, h); err != nil {
		t.Fatal(err)
	}
	if err := storage.SweepExpiredSecrets(db, now); err != nil {
		t.Fatal(err)
	}
	var raw storage.SecretHandoff
	if err := db.First(&raw, "id = ?", "sid-sweep").Error; err != nil {
		t.Fatal(err)
	}
	if raw.EnvelopeJSON != "" {
		t.Fatal("sweep left the expired blob in place")
	}
}
