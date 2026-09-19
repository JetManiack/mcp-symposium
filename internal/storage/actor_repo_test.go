package storage_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	if pgDSN := os.Getenv("TEST_POSTGRES_DSN"); pgDSN != "" {
		return openTestPostgresDB(t, pgDSN)
	}
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return db
}

func TestIssueAndAuthenticateAgentToken(t *testing.T) {
	db := openTestDB(t)

	actor, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	if actor.Kind != storage.ActorKindAgent {
		t.Errorf("Kind = %q, want %q", actor.Kind, storage.ActorKindAgent)
	}

	token, err := auth.Issue(db, actor.ID)
	if err != nil {
		t.Fatalf("auth.Issue() error = %v", err)
	}
	if token == "" {
		t.Fatal("auth.Issue() returned empty token")
	}

	authenticated, err := auth.Authenticate(db, token)
	if err != nil {
		t.Fatalf("auth.Authenticate() error = %v", err)
	}
	if authenticated.ID != actor.ID {
		t.Errorf("authenticated.ID = %q, want %q", authenticated.ID, actor.ID)
	}
}

func TestAuthenticateAgentToken_RejectsUnknownToken(t *testing.T) {
	db := openTestDB(t)

	if _, err := auth.Authenticate(db, "arp_does-not-exist"); err != storage.ErrInvalidToken {
		t.Errorf("auth.Authenticate() error = %v, want %v", err, storage.ErrInvalidToken)
	}
}

func TestCreateAgent_RejectsEmptyOrWhitespaceOnlyDisplayName(t *testing.T) {
	db := openTestDB(t)

	if _, err := storage.CreateAgent(db, ""); !errors.Is(err, storage.ErrEmptyDisplayName) {
		t.Errorf("CreateAgent(\"\") error = %v, want %v", err, storage.ErrEmptyDisplayName)
	}
	if _, err := storage.CreateAgent(db, "   "); !errors.Is(err, storage.ErrEmptyDisplayName) {
		t.Errorf("CreateAgent(\"   \") error = %v, want %v", err, storage.ErrEmptyDisplayName)
	}
}

func TestCreateAgent_RejectsDuplicateDisplayName(t *testing.T) {
	db := openTestDB(t)

	if _, err := storage.CreateAgent(db, "agent-a"); err != nil {
		t.Fatalf("first CreateAgent() error = %v", err)
	}
	if _, err := storage.CreateAgent(db, "agent-a"); err == nil {
		t.Fatal("second CreateAgent() error = nil, want a non-nil error for a duplicate display name")
	}
}

func TestRevokeAgentToken_PreventsFurtherAuthentication(t *testing.T) {
	db := openTestDB(t)
	actor, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	token, err := auth.Issue(db, actor.ID)
	if err != nil {
		t.Fatalf("auth.Issue() error = %v", err)
	}

	var cred storage.AgentCredential
	if err := db.First(&cred, "actor_id = ?", actor.ID).Error; err != nil {
		t.Fatalf("First(credential) error = %v", err)
	}

	if err := storage.RevokeAgentToken(db, cred.ID); err != nil {
		t.Fatalf("RevokeAgentToken() error = %v", err)
	}

	if _, err := auth.Authenticate(db, token); err != storage.ErrInvalidToken {
		t.Errorf("auth.Authenticate() error = %v, want %v", err, storage.ErrInvalidToken)
	}
}

func TestRevokeAllAgentCredentials_RevokesEveryTokenForTheActor(t *testing.T) {
	db := openTestDB(t)
	actor, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	tokenA, err := auth.Issue(db, actor.ID)
	if err != nil {
		t.Fatalf("auth.Issue(1) error = %v", err)
	}
	tokenB, err := auth.Issue(db, actor.ID)
	if err != nil {
		t.Fatalf("auth.Issue(2) error = %v", err)
	}

	if err := storage.RevokeAllAgentCredentials(db, actor.ID); err != nil {
		t.Fatalf("RevokeAllAgentCredentials() error = %v", err)
	}

	if _, err := auth.Authenticate(db, tokenA); err != storage.ErrInvalidToken {
		t.Errorf("auth.Authenticate(tokenA) error = %v, want %v", err, storage.ErrInvalidToken)
	}
	if _, err := auth.Authenticate(db, tokenB); err != storage.ErrInvalidToken {
		t.Errorf("auth.Authenticate(tokenB) error = %v, want %v", err, storage.ErrInvalidToken)
	}
}
