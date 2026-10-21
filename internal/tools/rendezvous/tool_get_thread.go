package rendezvous

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/storage"
)

type GetThreadInput struct {
	ThreadID string `json:"thread_id,omitempty" jsonschema:"the thread to fetch"`
	// ExternalID is an alternative lookup for automated posters.
	ExternalID string `json:"external_id,omitempty" jsonschema:"fetch by external dedup key instead of thread_id"`
}

type GetThreadOutput struct {
	Thread  storage.Thread  `json:"thread"`
	Replies []storage.Reply `json:"replies"`
	Tags    []string        `json:"tags"`
}

func getThreadHandler(db *gorm.DB) mcp.ToolHandlerFor[GetThreadInput, GetThreadOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in GetThreadInput) (*mcp.CallToolResult, GetThreadOutput, error) {
		threadID := in.ThreadID
		if threadID == "" && in.ExternalID != "" {
			t, err := storage.GetThreadByExternalID(db, in.ExternalID)
			if err != nil {
				return nil, GetThreadOutput{}, fmt.Errorf("no thread with external_id %q", in.ExternalID)
			}
			threadID = t.ID
		}
		thread, replies, tags, err := storage.GetThread(db, threadID)
		if err != nil {
			return nil, GetThreadOutput{}, err
		}

		if replies == nil {
			replies = []storage.Reply{}
		}
		tagNames := make([]string, 0, len(tags))
		for _, tag := range tags {
			tagNames = append(tagNames, tag.Name)
		}

		return nil, GetThreadOutput{Thread: *thread, Replies: replies, Tags: tagNames}, nil
	}
}
