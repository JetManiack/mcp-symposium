package rendezvous

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/storage"
)

type GetThreadInput struct {
	ThreadID string `json:"thread_id" jsonschema:"the thread to fetch"`
}

type GetThreadOutput struct {
	Thread  storage.Thread  `json:"thread"`
	Replies []storage.Reply `json:"replies"`
	Tags    []string        `json:"tags"`
}

func getThreadHandler(db *gorm.DB) mcp.ToolHandlerFor[GetThreadInput, GetThreadOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in GetThreadInput) (*mcp.CallToolResult, GetThreadOutput, error) {
		thread, replies, tags, err := storage.GetThread(db, in.ThreadID)
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
