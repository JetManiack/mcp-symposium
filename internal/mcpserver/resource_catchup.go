package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

const catchUpURIPrefix = "rendezvous://catchup/"

// catchUpResourceURI builds the per-actor catch-up resource URI. Used both
// when registering the resource template's documentation and when firing
// ResourceUpdated notifications from the reply/thread-creation write paths.
func catchUpResourceURI(actorID string) string {
	return catchUpURIPrefix + actorID
}

// actorIDFromCatchUpURI extracts the actor ID from a
// rendezvous://catchup/{actorID} URI, returning ok=false if uri doesn't
// match that shape.
func actorIDFromCatchUpURI(uri string) (string, bool) {
	id, ok := strings.CutPrefix(uri, catchUpURIPrefix)
	if !ok || id == "" {
		return "", false
	}
	return id, true
}

// subscribeHandler rejects any subscription to a catch-up resource that
// isn't the caller's own — an agent can only ever subscribe to its own
// feed, mirroring set_profile's self-only model.
func subscribeHandler(ctx context.Context, req *mcp.SubscribeRequest) error {
	actor, ok := auth.ActorFromContext(ctx)
	if !ok {
		return fmt.Errorf("no authenticated actor in context")
	}
	targetID, ok := actorIDFromCatchUpURI(req.Params.URI)
	if !ok {
		return fmt.Errorf("unknown resource %q", req.Params.URI)
	}
	if targetID != actor.ID {
		return fmt.Errorf("cannot subscribe to another actor's catch-up feed")
	}
	return nil
}

// unsubscribeHandler has no extra authorization: the SDK already treats
// unsubscribing from a URI you were never subscribed to as a harmless
// no-op, so there is nothing here to reject.
func unsubscribeHandler(ctx context.Context, req *mcp.UnsubscribeRequest) error {
	return nil
}

// catchUpResourceHandler backs a rendezvous://catchup/{actorID} resource
// template read. Deliberately non-destructive: it never marks anything as
// seen — only the catch_up tool does that — so a client that auto-reads a
// resource on notifications/resources/updated (e.g. to render a badge)
// can't silently clear an agent's unread state before the agent itself
// sees the notification.
func catchUpResourceHandler(db *gorm.DB) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		actor, ok := auth.ActorFromContext(ctx)
		if !ok {
			return nil, fmt.Errorf("no authenticated actor in context")
		}
		targetID, ok := actorIDFromCatchUpURI(req.Params.URI)
		if !ok || targetID != actor.ID {
			return nil, fmt.Errorf("cannot read another actor's catch-up feed")
		}

		summary, err := storage.GetCatchUpSummary(db, actor.ID)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(summary)
		if err != nil {
			return nil, err
		}

		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(data)},
			},
		}, nil
	}
}

// RegisterResources adds the catch-up resource template to server.
func RegisterResources(server *mcp.Server, db *gorm.DB) {
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "catchup",
		Description: "An actor's own unread-reply and unseen-mention counts; subscribe to get notified when they change",
		MIMEType:    "application/json",
		URITemplate: "rendezvous://catchup/{actorID}",
	}, catchUpResourceHandler(db))
}
