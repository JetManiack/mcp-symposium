package storage_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"mcp-symposium/internal/storage"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// openTestPostgresDB creates a uniquely-named schema on the Postgres
// instance at pgDSN, opens storage against it with that schema as the
// connection's search_path, and registers a cleanup to drop the schema —
// giving each test the same full isolation SQLite's per-test temp file
// gives it, without needing a separate database per test.
//
// pgDSN must not already contain a "?" query string (the docker-compose.yml
// DSN in this repo, e.g. "postgres://rendezvous:rendezvous@localhost:5432/rendezvous_test",
// satisfies this).
func openTestPostgresDB(t *testing.T, pgDSN string) *gorm.DB {
	t.Helper()

	schema := fmt.Sprintf("test_%d", rand.Int63())

	setup, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect to set up test schema: %v", err)
	}
	if err := setup.Exec(fmt.Sprintf("CREATE SCHEMA %s", schema)).Error; err != nil {
		t.Fatalf("CREATE SCHEMA %s: %v", schema, err)
	}
	t.Cleanup(func() {
		setup.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schema))
	})

	db, err := storage.Open(pgDSN + "?search_path=" + schema)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return db
}

func TestOpen_CreatesActorTableOnPostgres(t *testing.T) {
	pgDSN := os.Getenv("TEST_POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("TEST_POSTGRES_DSN not set; skipping Postgres-backed test — run `docker compose up -d` and set TEST_POSTGRES_DSN=postgres://rendezvous:rendezvous@localhost:5432/rendezvous_test to enable")
	}

	db := openTestPostgresDB(t, pgDSN)

	actor := &storage.Actor{
		ID:          "actor-1",
		DisplayName: "agent-a",
		Kind:        storage.ActorKindAgent,
	}
	if err := db.Create(actor).Error; err != nil {
		t.Fatalf("Create(actor) error = %v", err)
	}

	var fetched storage.Actor
	if err := db.First(&fetched, "id = ?", "actor-1").Error; err != nil {
		t.Fatalf("First(actor) error = %v", err)
	}
	if fetched.DisplayName != "agent-a" {
		t.Errorf("DisplayName = %q, want %q", fetched.DisplayName, "agent-a")
	}
	if fetched.CreatedAt.IsZero() {
		t.Error("CreatedAt was not set")
	}
}

// TestOpen_WaitsForAdvisoryLockBeforeMigrating simulates a second replica
// already running AutoMigrate (represented here by holding the same
// advisory lock storage.Open uses) and checks that a concurrent Open()
// against the same schema blocks until that lock is released, instead of
// racing it — the condition that currently makes strategy: Recreate
// mandatory (board thread 1086abc1).
func TestOpen_WaitsForAdvisoryLockBeforeMigrating(t *testing.T) {
	pgDSN := os.Getenv("TEST_POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("TEST_POSTGRES_DSN not set; skipping Postgres-backed test — run `docker compose up -d` and set TEST_POSTGRES_DSN=postgres://rendezvous:rendezvous@localhost:5432/rendezvous_test to enable")
	}

	schema := fmt.Sprintf("test_lock_%d", rand.Int63())
	setup, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect to set up test schema: %v", err)
	}
	if err := setup.Exec(fmt.Sprintf("CREATE SCHEMA %s", schema)).Error; err != nil {
		t.Fatalf("CREATE SCHEMA %s: %v", schema, err)
	}
	t.Cleanup(func() {
		setup.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schema))
	})

	setupSQLDB, err := setup.DB()
	if err != nil {
		t.Fatalf("setup.DB() error = %v", err)
	}
	ctx := context.Background()
	conn, err := setupSQLDB.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn() error = %v", err)
	}
	defer conn.Close()

	// Hold the same advisory lock storage.Open acquires, simulating a
	// second replica already mid-AutoMigrate.
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", storage.MigrationAdvisoryLockKey); err != nil {
		t.Fatalf("pg_advisory_lock: %v", err)
	}

	dsn := pgDSN + "?search_path=" + schema
	openDone := make(chan error, 1)
	go func() {
		_, err := storage.Open(dsn)
		openDone <- err
	}()

	select {
	case err := <-openDone:
		t.Fatalf("Open() returned (err=%v) before the advisory lock was released; it should have blocked", err)
	case <-time.After(300 * time.Millisecond):
		// Expected: Open() is still waiting on the lock.
	}

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", storage.MigrationAdvisoryLockKey); err != nil {
		t.Fatalf("pg_advisory_unlock: %v", err)
	}

	select {
	case err := <-openDone:
		if err != nil {
			t.Fatalf("Open() error after the lock was released = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Open() did not complete within 5s of the advisory lock being released")
	}
}
