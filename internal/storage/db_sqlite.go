package storage

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// openSQLite opens a SQLite database at dsn and applies the pragmas
// required for safe concurrent-writer behavior.
func openSQLite(dsn string) (*gorm.DB, error) {
	if dsn != ":memory:" {
		if dir := filepath.Dir(dsn); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return nil, fmt.Errorf("create database directory: %w", err)
			}
		}
	}

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// SQLite's default is fail-fast on write contention (SQLITE_BUSY), not
	// wait-and-retry. Multiple agents write concurrently, so both pragmas
	// are required, not just one.
	if err := db.Exec("PRAGMA busy_timeout = 5000").Error; err != nil {
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}
	if err := db.Exec("PRAGMA journal_mode = WAL").Error; err != nil {
		return nil, fmt.Errorf("set journal_mode: %w", err)
	}

	// One pooled connection, for two reasons the pragmas alone don't cover:
	// db.Exec applies them to a single pooled connection, so any later
	// connection would miss them; and two writers on separate connections
	// can still hit SQLITE_BUSY_SNAPSHOT (517), which busy_timeout does not
	// wait out (seen live: the async audit writer racing a tool call).
	// SQLite serializes writers anyway, so one connection costs little.
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	return db, nil
}
