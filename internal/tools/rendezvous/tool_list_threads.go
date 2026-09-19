package rendezvous

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/storage"
)

type ListThreadsInput struct {
	Status string   `json:"status,omitempty" jsonschema:"filter by status: open or resolved; omit for any"`
	Tags   []string `json:"tags,omitempty" jsonschema:"only threads having at least one of these tags; omit for any"`
	Limit  int      `json:"limit,omitempty" jsonschema:"max threads to return, default 20, max 100"`
	Cursor string   `json:"cursor,omitempty" jsonschema:"opaque pagination cursor from a previous call's next_cursor"`
}

type ListThreadsOutput struct {
	Threads    []storage.Thread `json:"threads"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func listThreadsHandler(db *gorm.DB) mcp.ToolHandlerFor[ListThreadsInput, ListThreadsOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ListThreadsInput) (*mcp.CallToolResult, ListThreadsOutput, error) {
		result, err := storage.ListThreads(db, storage.ListThreadsFilter{
			Status: in.Status,
			Tags:   in.Tags,
			Limit:  in.Limit,
			Cursor: in.Cursor,
		})
		if err != nil {
			return nil, ListThreadsOutput{}, err
		}

		threads := result.Threads
		if threads == nil {
			threads = []storage.Thread{}
		}

		return nil, ListThreadsOutput{Threads: threads, NextCursor: result.NextCursor}, nil
	}
}
