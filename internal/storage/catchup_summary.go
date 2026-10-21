package storage

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// CatchUpSummary is a non-destructive count of what CatchUp would return
// for an actor — used to back a read-only resource so a client peeking at
// it (e.g. to render a badge) can't accidentally mark anything as seen.
type CatchUpSummary struct {
	UnreadReplyCount   int `json:"unread_reply_count"`
	UnseenMentionCount int `json:"unseen_mention_count"`
	// UnseenHandoffAlertCount counts security events on this actor's secret
	// handoffs (foreign/double fetch) not yet collected via catch_up.
	UnseenHandoffAlertCount int `json:"unseen_handoff_alert_count"`
}

// GetCatchUpSummary mirrors CatchUp's read logic exactly, minus the
// transaction that marks replies/mentions as read/seen.
func GetCatchUpSummary(db *gorm.DB, actorID string) (*CatchUpSummary, error) {
	var watches []ThreadWatch
	if err := db.Where("actor_id = ?", actorID).Find(&watches).Error; err != nil {
		return nil, err
	}

	summary := &CatchUpSummary{}
	for _, w := range watches {
		var count int64
		if err := db.Model(&Reply{}).
			Where("thread_id = ? AND created_at > ?", w.ThreadID, w.LastReadAt).
			Count(&count).Error; err != nil {
			return nil, err
		}
		summary.UnreadReplyCount += int(count)
	}

	var mentionCount int64
	if err := db.Model(&Mention{}).
		Where("mentioned_actor_id = ? AND seen_at IS NULL", actorID).
		Count(&mentionCount).Error; err != nil {
		return nil, err
	}
	summary.UnseenMentionCount = int(mentionCount)

	alertCount, err := CountUnseenHandoffAlerts(db, actorID)
	if err != nil {
		return nil, err
	}
	summary.UnseenHandoffAlertCount = alertCount

	return summary, nil
}

// LatestActivityAt returns the created_at of the newest unread reply or
// unseen mention for actorID, or nil when there is nothing new. Like
// GetCatchUpSummary it reads without marking anything.
func LatestActivityAt(db *gorm.DB, actorID string) (*time.Time, error) {
	var latest *time.Time

	var watches []ThreadWatch
	if err := db.Where("actor_id = ?", actorID).Find(&watches).Error; err != nil {
		return nil, err
	}
	for _, w := range watches {
		var reply Reply
		err := db.Where("thread_id = ? AND created_at > ?", w.ThreadID, w.LastReadAt).
			Order("created_at desc").First(&reply).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		if latest == nil || reply.CreatedAt.After(*latest) {
			t := reply.CreatedAt
			latest = &t
		}
	}

	var mention Mention
	err := db.Where("mentioned_actor_id = ? AND seen_at IS NULL", actorID).
		Order("created_at desc").First(&mention).Error
	if err == nil {
		if latest == nil || mention.CreatedAt.After(*latest) {
			t := mention.CreatedAt
			latest = &t
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	return latest, nil
}

// WatchersOf returns every actor ID currently watching threadID.
func WatchersOf(db *gorm.DB, threadID string) ([]string, error) {
	var ids []string
	if err := db.Model(&Watcher{}).Where("thread_id = ?", threadID).Pluck("actor_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// MentionedActorIDsForReply returns the actor IDs mentioned in replyID's
// body (the mentions createMentions created for that specific reply).
func MentionedActorIDsForReply(db *gorm.DB, replyID string) ([]string, error) {
	var ids []string
	if err := db.Model(&Mention{}).Where("reply_id = ?", replyID).Pluck("mentioned_actor_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// MentionedActorIDsForThread returns the actor IDs mentioned in threadID's
// opening post body.
func MentionedActorIDsForThread(db *gorm.DB, threadID string) ([]string, error) {
	var ids []string
	// reply_id IS NULL: reply mentions carry thread_id too, and this must
	// return only the opening post's mentions.
	if err := db.Model(&Mention{}).Where("thread_id = ? AND reply_id IS NULL", threadID).Pluck("mentioned_actor_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}
