package storage

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrEmptyTitle = errors.New("title must not be empty")
	ErrEmptyBody  = errors.New("body must not be empty")
)

func CreateThread(db *gorm.DB, authorID, title, body string, tags []string) (*Thread, error) {
	if strings.TrimSpace(title) == "" {
		return nil, ErrEmptyTitle
	}
	if strings.TrimSpace(body) == "" {
		return nil, ErrEmptyBody
	}

	thread := &Thread{
		ID:       uuid.NewString(),
		Title:    title,
		Body:     body,
		Status:   "open",
		AuthorID: authorID,
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(thread).Error; err != nil {
			return err
		}
		if err := tx.Create(&Watcher{ThreadID: thread.ID, ActorID: authorID}).Error; err != nil {
			return err
		}
		if err := tx.Create(&ThreadWatch{
			ActorID:    authorID,
			ThreadID:   thread.ID,
			LastReadAt: thread.CreatedAt,
		}).Error; err != nil {
			return err
		}
		if err := attachTags(tx, thread.ID, tags); err != nil {
			return err
		}
		report, err := createMentions(tx, nil, &thread.ID, authorID, body)
		thread.MentionReport = report
		return err
	})
	if err != nil {
		return nil, err
	}
	return thread, nil
}

// attachTags finds-or-creates each named tag and links it to threadID.
// Reusing an existing tag by name (rather than always inserting) keeps the
// Tag table free of duplicates when multiple threads share a tag.
func attachTags(tx *gorm.DB, threadID string, tagNames []string) error {
	for _, name := range tagNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&Tag{ID: uuid.NewString(), Name: name}).Error; err != nil {
			return err
		}

		var tag Tag
		if err := tx.Where("name = ?", name).First(&tag).Error; err != nil {
			return err
		}

		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&ThreadTag{ThreadID: threadID, TagID: tag.ID}).Error; err != nil {
			return err
		}
	}
	return nil
}

var (
	mentionPattern         = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)
	fencedCodeBlockPattern = regexp.MustCompile("(?s)```.*?```")
	inlineCodeSpanPattern  = regexp.MustCompile("`[^`\n]*`")
)

// stripCodeRegions blanks out fenced code blocks and inline code spans so
// mention scanning ignores @handles that only appear as pasted example
// text (logs, diffs, manifests) rather than as a genuine ping. Only the
// scan sees this transformed copy — the original body is stored and
// rendered unchanged. Fenced blocks are stripped first so any backticks
// inside one don't confuse the inline-span pass over what's left.
func stripCodeRegions(body string) string {
	body = fencedCodeBlockPattern.ReplaceAllStringFunc(body, blankOut)
	body = inlineCodeSpanPattern.ReplaceAllStringFunc(body, blankOut)
	return body
}

func blankOut(s string) string {
	return strings.Repeat(" ", len(s))
}

func AddReply(db *gorm.DB, threadID, authorID, body string, extraWatcherIDs []string) (*Reply, error) {
	if strings.TrimSpace(body) == "" {
		return nil, ErrEmptyBody
	}

	reply := &Reply{
		ID:       uuid.NewString(),
		ThreadID: threadID,
		Body:     body,
		AuthorID: authorID,
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		var thread Thread
		if err := tx.First(&thread, "id = ?", threadID).Error; err != nil {
			return fmt.Errorf("thread %q not found: %w", threadID, err)
		}

		if err := tx.Create(reply).Error; err != nil {
			return err
		}

		watcherIDs := append([]string{authorID}, extraWatcherIDs...)
		for _, watcherID := range watcherIDs {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
				Create(&Watcher{ThreadID: threadID, ActorID: watcherID}).Error; err != nil {
				return err
			}
		}

		if err := upsertThreadWatch(tx, authorID, threadID, reply.CreatedAt); err != nil {
			return err
		}

		// Extra watchers (excluding the author, handled above with the
		// reply's own timestamp) get a ThreadWatch row only if they don't
		// already have one, so the whole thread history shows up as unread
		// for someone newly added as a watcher, without resetting the read
		// cursor of someone already watching.
		for _, watcherID := range extraWatcherIDs {
			if watcherID == authorID {
				continue
			}
			if err := ensureThreadWatch(tx, watcherID, threadID); err != nil {
				return err
			}
		}

		// Pass the thread ID too: a mention must always carry thread_id, or
		// recipients of a reply mention can't resolve it without a search.
		report, err := createMentions(tx, &reply.ID, &threadID, authorID, body)
		reply.MentionReport = report
		return err
	})
	if err != nil {
		return nil, err
	}
	return reply, nil
}

func upsertThreadWatch(tx *gorm.DB, actorID, threadID string, readAt time.Time) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "actor_id"}, {Name: "thread_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_read_at", "updated_at"}),
	}).Create(&ThreadWatch{
		ActorID:    actorID,
		ThreadID:   threadID,
		LastReadAt: readAt,
	}).Error
}

// ensureThreadWatch creates a ThreadWatch row for actorID/threadID if one
// doesn't already exist, leaving LastReadAt as the zero time.Time so the
// entire thread history is treated as unread. It does nothing if a row
// already exists, so it never resets an existing read cursor.
func ensureThreadWatch(tx *gorm.DB, actorID, threadID string) error {
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ThreadWatch{
		ActorID:  actorID,
		ThreadID: threadID,
		// LastReadAt left as zero time.Time: a newly-added watcher hasn't
		// read anything in this thread yet, so everything is unread.
	}).Error
}

// MentionReport classifies every @handle found in a thread body or reply
// into those that matched an actor and those that didn't (typo, wrong
// case that still didn't fold to a match, or ambiguous after folding) —
// so a sender can tell a delivered mention from a silently dropped one
// without cross-referencing the mentions table.
type MentionReport struct {
	Resolved   []string `json:"resolved"`
	Unresolved []string `json:"unresolved"`
}

func createMentions(tx *gorm.DB, replyID, threadID *string, authorID, body string) (MentionReport, error) {
	var report MentionReport

	matches := mentionPattern.FindAllStringSubmatch(stripCodeRegions(body), -1)
	if len(matches) == 0 {
		return report, nil
	}

	seenNames := map[string]bool{}  // skip redundant lookups for a literally-repeated handle
	seenActors := map[string]bool{} // the actual notify-once guarantee: one row per actor per post
	for _, m := range matches {
		name := m[1]
		if seenNames[name] {
			continue
		}
		seenNames[name] = true

		actor, ok, err := resolveMentionTarget(tx, name)
		if err != nil {
			return report, err
		}
		if !ok {
			report.Unresolved = append(report.Unresolved, name)
			continue // unresolvable or ambiguous @name in free-form body text; not an error
		}
		report.Resolved = append(report.Resolved, name)

		if actor.ID == authorID {
			continue // don't notify yourself
		}
		if seenActors[actor.ID] {
			continue // same actor already mentioned via a different spelling in this post
		}
		seenActors[actor.ID] = true

		mention := &Mention{
			ID:               uuid.NewString(),
			ReplyID:          replyID,
			ThreadID:         threadID,
			MentionedActorID: actor.ID,
		}
		if err := tx.Create(mention).Error; err != nil {
			return report, err
		}
	}
	return report, nil
}

// resolveMentionTarget resolves an @name to an Actor. Precedence: exact
// nickname, exact display_name, case-folded nickname, case-folded
// display_name — exact matches win over folded ones, and nickname (the
// onboarded, identifier-shaped field) wins over the original
// display_name, so actors remain mentionable by their original name even
// before (or without ever) setting a profile.
//
// A folded lookup that matches more than one actor (e.g. two actors whose
// nicknames differ only by case) is ambiguous and resolves to nobody —
// ok=false — rather than picking one arbitrarily. Both nickname and
// display_name are UNIQUE columns, so only the case-insensitive lookups
// can ever be ambiguous; exact lookups never are.
func resolveMentionTarget(tx *gorm.DB, name string) (actor Actor, ok bool, err error) {
	if actor, ok, err = actorByExactNickname(tx, name); err != nil || ok {
		return actor, ok, err
	}
	if actor, ok, err = actorByExactDisplayName(tx, name); err != nil || ok {
		return actor, ok, err
	}

	foldedActor, ambiguous, err := actorByFoldedNickname(tx, name)
	if err != nil {
		return Actor{}, false, err
	}
	if ambiguous {
		return Actor{}, false, nil
	}
	if foldedActor != nil {
		return *foldedActor, true, nil
	}

	foldedActor, ambiguous, err = actorByFoldedDisplayName(tx, name)
	if err != nil {
		return Actor{}, false, err
	}
	if ambiguous {
		return Actor{}, false, nil
	}
	if foldedActor != nil {
		return *foldedActor, true, nil
	}

	return Actor{}, false, nil
}

func actorByExactNickname(tx *gorm.DB, name string) (Actor, bool, error) {
	var profile ActorProfile
	err := tx.Where("nickname = ?", name).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Actor{}, false, nil
	}
	if err != nil {
		return Actor{}, false, err
	}
	var actor Actor
	if err := tx.First(&actor, "id = ?", profile.ActorID).Error; err != nil {
		return Actor{}, false, err
	}
	return actor, true, nil
}

func actorByExactDisplayName(tx *gorm.DB, name string) (Actor, bool, error) {
	var actor Actor
	err := tx.Where("display_name = ?", name).First(&actor).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Actor{}, false, nil
	}
	if err != nil {
		return Actor{}, false, err
	}
	return actor, true, nil
}

// actorByFoldedNickname case-insensitively matches name against
// ActorProfile.Nickname. ambiguous=true means more than one profile
// matched and the caller must not pick one arbitrarily.
func actorByFoldedNickname(tx *gorm.DB, name string) (matched *Actor, ambiguous bool, err error) {
	var profiles []ActorProfile
	if err := tx.Where("lower(nickname) = lower(?)", name).Find(&profiles).Error; err != nil {
		return nil, false, err
	}
	if len(profiles) == 0 {
		return nil, false, nil
	}
	if len(profiles) > 1 {
		return nil, true, nil
	}
	var actor Actor
	if err := tx.First(&actor, "id = ?", profiles[0].ActorID).Error; err != nil {
		return nil, false, err
	}
	return &actor, false, nil
}

// actorByFoldedDisplayName case-insensitively matches name against
// Actor.DisplayName. ambiguous=true means more than one actor matched
// and the caller must not pick one arbitrarily.
func actorByFoldedDisplayName(tx *gorm.DB, name string) (matched *Actor, ambiguous bool, err error) {
	var actors []Actor
	if err := tx.Where("lower(display_name) = lower(?)", name).Find(&actors).Error; err != nil {
		return nil, false, err
	}
	if len(actors) == 0 {
		return nil, false, nil
	}
	if len(actors) > 1 {
		return nil, true, nil
	}
	return &actors[0], false, nil
}
