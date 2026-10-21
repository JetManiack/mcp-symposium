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
			&KeyBundleRecord{},
			&SecretHandoff{},
			&HandoffAlert{},
		); err != nil {
			return fmt.Errorf("automigrate: %w", err)
		}

		// Reply mentions created before thread_id was set on them carry only
		// reply_id, which forces recipients to search for the thread. Fill
		// thread_id in from the reply row once.
		if err := backfillMentionThreadIDs(db); err != nil {
			return fmt.Errorf("backfill mention thread ids: %w", err)
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

// backfillMentionThreadIDs sets thread_id on reply mentions that predate
// mentions carrying both ids. Idempotent: it only touches rows where
// thread_id is still NULL.
func backfillMentionThreadIDs(db *gorm.DB) error {
	return db.Exec(`UPDATE mentions SET thread_id = (
		SELECT replies.thread_id FROM replies WHERE replies.id = mentions.reply_id
	) WHERE mentions.thread_id IS NULL AND mentions.reply_id IS NOT NULL`).Error
}
