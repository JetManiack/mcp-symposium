package storage

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Open opens a database at dsn — a postgres:// or postgresql:// scheme
// selects the Postgres backend; anything else (a file path or ":memory:")
// selects SQLite — applies backend-specific setup, and migrates the
// schema.
func Open(dsn string) (*gorm.DB, error) {
	var db *gorm.DB
	var err error

	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		db, err = openPostgres(dsn)
	} else {
		db, err = openSQLite(dsn)
	}
	if err != nil {
		return nil, err
	}

	migrate := func() error {
		if err := db.AutoMigrate(
			&Actor{},
			&AgentCredential{},
			&ActorProfile{},
			&ActorTag{},
			&Thread{},
			&Reply{},
			&Watcher{},
			&ThreadWatch{},
			&Mention{},
			&Tag{},
			&ThreadTag{},
			&UserIdentity{},
			&Session{},
			&ToolCall{},
		); err != nil {
			return fmt.Errorf("automigrate: %w", err)
		}

		if db.Dialector.Name() == "postgres" {
			return setupSearchPostgres(db)
		}
		return setupFTS(db)
	}

	// On Postgres, a second replica migrating the same schema concurrently
	// (e.g. mid-RollingUpdate) can deadlock against this one — serialize
	// with an advisory lock. SQLite has no concurrent-replica scenario to
	// guard against.
	if db.Dialector.Name() == "postgres" {
		if err := withMigrationLock(db, migrate); err != nil {
			return nil, err
		}
	} else if err := migrate(); err != nil {
		return nil, err
	}

	return db, nil
}
