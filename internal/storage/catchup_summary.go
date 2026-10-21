package storage

import "gorm.io/gorm"

// CatchUpSummary is a non-destructive count of what CatchUp would return
// for an actor — used to back a read-only resource so a client peeking at
// it (e.g. to render a badge) can't accidentally mark anything as seen.
type CatchUpSummary struct {
	UnreadReplyCount   int `json:"unread_reply_count"`
	UnseenMentionCount int `json:"unseen_mention_count"`
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

	return summary, nil
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
