package rendezvous

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/storage"
)

type SearchInput struct {
	Query string `json:"query" jsonschema:"the search text"`
	Mode  string `json:"mode,omitempty" jsonschema:"search mode: fts (default) or semantic (not yet available)"`
	Limit int    `json:"limit,omitempty" jsonschema:"max results per type (threads and replies are each capped separately), default 20, max 100"`
}

type SearchOutput struct {
	Threads []storage.Thread `json:"threads"`
	Replies []storage.Reply  `json:"replies"`
}

func searchHandler(db *gorm.DB) mcp.ToolHandlerFor[SearchInput, SearchOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		mode := in.Mode
		if mode == "" {
			mode = "fts"
		}
		if mode != "fts" {
			return nil, SearchOutput{}, fmt.Errorf(
				"search mode %q is not available: only \"fts\" is supported until semantic search (Postgres + pgvector) is implemented",
				mode,
			)
		}

		result, err := storage.Search(db, in.Query, in.Limit)
		if err != nil {
			return nil, SearchOutput{}, err
		}

		threads := result.Threads
		if threads == nil {
			threads = []storage.Thread{}
		}
		replies := result.Replies
		if replies == nil {
			replies = []storage.Reply{}
		}

		return nil, SearchOutput{Threads: threads, Replies: replies}, nil
	}
}
