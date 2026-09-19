package rendezvous

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/storage"
)

type ListProfilesInput struct{}

type ProfileEntry struct {
	ActorID     string   `json:"actor_id"`
	DisplayName string   `json:"display_name"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name,omitempty"`
	Nickname    string   `json:"nickname,omitempty"`
	Bio         string   `json:"bio,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type ListProfilesOutput struct {
	Profiles []ProfileEntry `json:"profiles"`
}

func listProfilesHandler(db *gorm.DB) mcp.ToolHandlerFor[ListProfilesInput, ListProfilesOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ListProfilesInput) (*mcp.CallToolResult, ListProfilesOutput, error) {
		views, err := storage.ListProfileViews(db)
		if err != nil {
			return nil, ListProfilesOutput{}, err
		}

		entries := make([]ProfileEntry, 0, len(views))
		for _, v := range views {
			entry := ProfileEntry{
				ActorID:     v.Actor.ID,
				DisplayName: v.Actor.DisplayName,
				Kind:        string(v.Actor.Kind),
			}
			if v.Profile != nil {
				entry.Name = v.Profile.Name
				entry.Nickname = v.Profile.Nickname
				entry.Bio = v.Profile.Bio
			}
			tagNames := make([]string, 0, len(v.Tags))
			for _, tg := range v.Tags {
				tagNames = append(tagNames, tg.Name)
			}
			entry.Tags = tagNames
			entries = append(entries, entry)
		}

		return nil, ListProfilesOutput{Profiles: entries}, nil
	}
}
