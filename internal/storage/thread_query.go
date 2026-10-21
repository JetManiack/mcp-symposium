package storage

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// GetThread fetches a thread, its replies (oldest first), and its tags.
func GetThread(db *gorm.DB, threadID string) (*Thread, []Reply, []Tag, error) {
	var thread Thread
	if err := db.First(&thread, "id = ?", threadID).Error; err != nil {
		return nil, nil, nil, err
	}

	var replies []Reply
	if err := db.Where("thread_id = ?", threadID).Order("created_at asc").Find(&replies).Error; err != nil {
		return nil, nil, nil, err
	}

	var tags []Tag
	if err := db.Joins("JOIN thread_tags ON thread_tags.tag_id = tags.id").
		Where("thread_tags.thread_id = ?", threadID).
		Find(&tags).Error; err != nil {
		return nil, nil, nil, err
	}

	return &thread, replies, tags, nil
}

type ListThreadsFilter struct {
	Status string   // "" matches any status
	Tags   []string // empty matches any; otherwise a thread must have at least one of these tags
	Limit  int      // <= 0 or > 100 is clamped to the default of 20
	Cursor string   // opaque, from a previous ListThreadsResult.NextCursor; "" starts from the newest thread
}

type ListThreadsResult struct {
	Threads    []Thread `json:"threads"`
	NextCursor string   `json:"next_cursor"` // "" means there are no more pages
}

// ListThreads returns threads newest-first, optionally filtered by status
// and/or tags, paginated via an opaque cursor.
func ListThreads(db *gorm.DB, filter ListThreadsFilter) (*ListThreadsResult, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	query := db.Model(&Thread{})

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	if len(filter.Tags) > 0 {
		tagThreadIDs := db.Table("thread_tags").
			Joins("JOIN tags ON tags.id = thread_tags.tag_id").
			Where("tags.name IN ?", filter.Tags).
			Select("thread_tags.thread_id")
		query = query.Where("threads.id IN (?)", tagThreadIDs)
	}

	if filter.Cursor != "" {
		cursorTime, cursorID, err := decodeThreadCursor(filter.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		query = query.Where("(created_at < ?) OR (created_at = ? AND id < ?)", cursorTime, cursorTime, cursorID)
	}

	var threads []Thread
	if err := query.Order("created_at desc, id desc").Limit(limit + 1).Find(&threads).Error; err != nil {
		return nil, err
	}

	result := &ListThreadsResult{}
	if len(threads) > limit {
		threads = threads[:limit]
		last := threads[len(threads)-1]
		result.NextCursor = encodeThreadCursor(last.CreatedAt, last.ID)
	}
	result.Threads = threads
	return result, nil
}

func encodeThreadCursor(t time.Time, id string) string {
	raw := t.Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeThreadCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("decode cursor: %w", err)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("malformed cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("parse cursor timestamp: %w", err)
	}
	return t, parts[1], nil
}

// GetThreadByExternalID resolves an external dedup key to its thread ID,
// or returns gorm.ErrRecordNotFound. Automated posters use it to upsert:
// reply into the existing thread instead of opening a duplicate.
func GetThreadByExternalID(db *gorm.DB, externalID string) (*Thread, error) {
	var t Thread
	if err := db.First(&t, "external_id = ?", externalID).Error; err != nil {
		return nil, err
	}
	return &t, nil
}
