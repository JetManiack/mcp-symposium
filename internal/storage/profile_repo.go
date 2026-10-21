package storage

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrEmptyName       = errors.New("name must not be empty")
	ErrEmptyNickname   = errors.New("nickname must not be empty")
	ErrInvalidNickname = errors.New("nickname may only contain letters, digits, underscores, and hyphens")
	ErrNicknameTaken   = errors.New("nickname is already taken")
)

var nicknamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ProfileView bundles an Actor with its optional profile (nil if the
// actor hasn't onboarded yet) and specialization tags.
type ProfileView struct {
	Actor   Actor
	Profile *ActorProfile
	Tags    []Tag
}

// UpsertActorProfile creates or updates actorID's profile. Re-upserting
// the caller's own current nickname is allowed (a no-op collision with
// self); any other actor already holding nickname is rejected.
func UpsertActorProfile(db *gorm.DB, actorID, name, nickname, bio string, tagNames []string) (*ActorProfile, error) {
	name = strings.TrimSpace(name)
	nickname = strings.TrimSpace(nickname)
	if name == "" {
		return nil, ErrEmptyName
	}
	if nickname == "" {
		return nil, ErrEmptyNickname
	}
	if !nicknamePattern.MatchString(nickname) {
		return nil, ErrInvalidNickname
	}

	profile := &ActorProfile{ActorID: actorID, Name: name, Nickname: nickname, Bio: bio}

	err := db.Transaction(func(tx *gorm.DB) error {
		var existing ActorProfile
		err := tx.Where("nickname = ? AND actor_id <> ?", nickname, actorID).First(&existing).Error
		if err == nil {
			return ErrNicknameTaken
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		// An actor with a published key bundle can't change its nickname:
		// the signed bundle carries the name next to the immutable id, and
		// humans compare fingerprints against that name. A rename would be
		// a new bundle anyway (new operator signature), so the order is:
		// rename first, then keygen + sign, then set_key_bundle.
		var own ActorProfile
		if err := tx.Where("actor_id = ?", actorID).First(&own).Error; err == nil && own.Nickname != nickname {
			var bundles int64
			if err := tx.Model(&KeyBundleRecord{}).Where("actor_id = ?", actorID).Count(&bundles).Error; err != nil {
				return err
			}
			if bundles > 0 {
				return fmt.Errorf("nickname is locked while a key bundle is published for this actor — the signed bundle names it; rotate the bundle (keygen + operator sign + set_key_bundle) as part of any rename")
			}
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "actor_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"name", "nickname", "bio", "updated_at"}),
		}).Create(profile).Error; err != nil {
			return err
		}

		if err := tx.Where("actor_id = ?", actorID).Delete(&ActorTag{}).Error; err != nil {
			return err
		}
		return attachActorTags(tx, actorID, tagNames)
	})
	if err != nil {
		return nil, err
	}
	return profile, nil
}

// attachActorTags finds-or-creates each named tag and links it to
// actorID. Mirrors attachTags (thread_repo.go) for the ActorTag join
// table.
func attachActorTags(tx *gorm.DB, actorID string, tagNames []string) error {
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
			Create(&ActorTag{ActorID: actorID, TagID: tag.ID}).Error; err != nil {
			return err
		}
	}
	return nil
}

// GetProfileView fetches actorID's Actor row plus its profile (nil if
// none) and specialization tags.
func GetProfileView(db *gorm.DB, actorID string) (*ProfileView, error) {
	var actor Actor
	if err := db.First(&actor, "id = ?", actorID).Error; err != nil {
		return nil, err
	}
	view := &ProfileView{Actor: actor}

	var profile ActorProfile
	err := db.Where("actor_id = ?", actorID).First(&profile).Error
	if err == nil {
		view.Profile = &profile
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var tags []Tag
	if err := db.Joins("JOIN actor_tags ON actor_tags.tag_id = tags.id").
		Where("actor_tags.actor_id = ?", actorID).
		Find(&tags).Error; err != nil {
		return nil, err
	}
	view.Tags = tags

	return view, nil
}

// ListProfileViews returns every actor (both kinds), ordered by display
// name, each with its profile (nil if not onboarded) and tags.
func ListProfileViews(db *gorm.DB) ([]ProfileView, error) {
	var actors []Actor
	if err := db.Order("display_name asc").Find(&actors).Error; err != nil {
		return nil, err
	}

	views := make([]ProfileView, 0, len(actors))
	for _, actor := range actors {
		view, err := GetProfileView(db, actor.ID)
		if err != nil {
			return nil, err
		}
		views = append(views, *view)
	}
	return views, nil
}
