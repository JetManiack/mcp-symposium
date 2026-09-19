package storage

import "time"

// ToolCall records a single MCP tool invocation for audit and observability.
type ToolCall struct {
	ID         string    `gorm:"type:char(36);primaryKey"`
	ActorID    string    `gorm:"type:char(36);index"`
	Tool       string    `gorm:"not null;index"`
	InputJSON  string    `gorm:"not null"`
	OutputJSON string    `gorm:"not null"`
	OutputSize int
	Truncated  bool
	IsError    bool
	DurationMS int64
	CalledAt   time.Time `gorm:"index"`
}

// ActorKind distinguishes an AI agent from a human user. Both share the
// Actor table so every other table (Thread, Reply, Watcher, Mention) needs
// only a single foreign key, regardless of which kind of actor it points to.
type ActorKind string

const (
	ActorKindAgent ActorKind = "agent"
	ActorKindHuman ActorKind = "human"
)

type Actor struct {
	ID          string    `gorm:"type:char(36);primaryKey" json:"id"`
	DisplayName string    `gorm:"not null;uniqueIndex" json:"display_name"`
	Kind        ActorKind `gorm:"type:varchar(10);not null;index" json:"kind"`
	CreatedAt   time.Time `json:"created_at"`
}

type AgentCredential struct {
	ID         string `gorm:"type:char(36);primaryKey"`
	ActorID    string `gorm:"type:char(36);not null;index"`
	TokenHash  string `gorm:"type:char(64);not null;uniqueIndex"`
	Label      string `gorm:"type:varchar(255)"`
	CreatedAt  time.Time
	RevokedAt  *time.Time
	LastUsedAt *time.Time
}

type Thread struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	Title     string    `gorm:"not null" json:"title"`
	Body      string    `gorm:"not null" json:"body"`
	Status    string    `gorm:"type:varchar(10);not null;default:open" json:"status"`
	AuthorID  string    `gorm:"type:char(36);not null;index" json:"author_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// MentionReport is populated by CreateThread for the caller's own use
	// (e.g. surfacing resolved/unresolved @handles in a tool response). It
	// is never persisted or serialized as part of the Thread itself.
	MentionReport MentionReport `gorm:"-" json:"-"`
}

type Reply struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	ThreadID  string    `gorm:"type:char(36);not null;index" json:"thread_id"`
	Body      string    `gorm:"not null" json:"body"`
	AuthorID  string    `gorm:"type:char(36);not null;index" json:"author_id"`
	CreatedAt time.Time `json:"created_at"`

	// MentionReport is populated by AddReply for the caller's own use (see
	// Thread.MentionReport). Never persisted or serialized.
	MentionReport MentionReport `gorm:"-" json:"-"`
}

type Watcher struct {
	ThreadID string `gorm:"type:char(36);primaryKey"`
	ActorID  string `gorm:"type:char(36);primaryKey"`
}

// ThreadWatch is the per-(actor,thread) read cursor used by catch_up.
type ThreadWatch struct {
	ActorID    string `gorm:"type:char(36);primaryKey"`
	ThreadID   string `gorm:"type:char(36);primaryKey"`
	LastReadAt time.Time
	UpdatedAt  time.Time
}

type Mention struct {
	ID               string     `gorm:"type:char(36);primaryKey" json:"id"`
	ReplyID          *string    `gorm:"type:char(36);index" json:"reply_id,omitempty"`
	ThreadID         *string    `gorm:"type:char(36);index" json:"thread_id,omitempty"`
	MentionedActorID string     `gorm:"type:char(36);not null;index" json:"mentioned_actor_id"`
	CreatedAt        time.Time  `json:"created_at"`
	SeenAt           *time.Time `json:"seen_at,omitempty"`
}

type Tag struct {
	ID   string `gorm:"type:char(36);primaryKey"`
	Name string `gorm:"not null;uniqueIndex"`
}

// ThreadTag is the many-to-many join between Thread and Tag.
type ThreadTag struct {
	ThreadID string `gorm:"type:char(36);primaryKey"`
	TagID    string `gorm:"type:char(36);primaryKey"`
}

// UserIdentity links a human Actor to whatever subject identifier the
// active humanauth.Provider produces. The field is named KeycloakSubject
// to match the eventual OIDC integration, but with the current stub
// provider it stores a fixed placeholder value, not a real Keycloak
// subject.
type UserIdentity struct {
	ActorID         string `gorm:"type:char(36);primaryKey"`
	KeycloakSubject string `gorm:"not null;uniqueIndex"`
	Role            string `gorm:"type:varchar(10);not null"`
}

// Session is a server-side OIDC login session, keyed by an opaque ID
// stored in the browser's session cookie. ExpiresAt is NOT the session's
// final expiry — it's a short checkpoint after which
// humanauth.OIDCProvider re-validates the session against Keycloak using
// RefreshToken (re-reading claims, so a role/group change propagates);
// the session's true maximum lifetime is bounded by how long Keycloak's
// own refresh token stays valid, which isn't tracked separately here.
type Session struct {
	ID           string `gorm:"type:char(64);primaryKey"` // holds a SHA-256 hex digest (humanauth.hashSessionID), not a UUID — 64 chars, not 36
	Subject      string `gorm:"not null;index"`
	DisplayName  string `gorm:"not null"`
	Role         string `gorm:"type:varchar(10);not null"`
	RefreshToken string `gorm:"not null"`
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

// ActorProfile is an actor's self-service onboarding profile: 1:1 with
// Actor via ActorID. Its absence (no row) means the actor hasn't
// onboarded yet — Actor itself stays a minimal identity row, matching
// AgentCredential/UserIdentity for kind-specific or optional data.
type ActorProfile struct {
	ActorID   string    `gorm:"type:char(36);primaryKey" json:"actor_id"`
	Name      string    `gorm:"not null" json:"name"`
	Nickname  string    `gorm:"not null;uniqueIndex" json:"nickname"`
	Bio       string    `json:"bio"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ActorTag is the many-to-many join between Actor and Tag, mirroring
// ThreadTag — actor specialization tags share the same Tag vocabulary
// threads use.
type ActorTag struct {
	ActorID string `gorm:"type:char(36);primaryKey"`
	TagID   string `gorm:"type:char(36);primaryKey"`
}
