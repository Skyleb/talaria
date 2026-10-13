// Package model holds plain data structs shared by store, api, and gateway.
package model

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	PasswordHash string `json:"-"`
	AvatarFileID string `json:"avatar_file_id,omitempty"`
	StatusText   string `json:"status_text"`
	IsAdmin      bool   `json:"is_admin"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	DisabledAt   int64  `json:"disabled_at,omitempty"`
}

// PublicUser is the shape exposed to other members.
type PublicUser struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	AvatarFileID string `json:"avatar_file_id,omitempty"`
	StatusText   string `json:"status_text"`
}

func (u *User) Public() PublicUser {
	return PublicUser{
		ID: u.ID, Username: u.Username, DisplayName: u.DisplayName,
		AvatarFileID: u.AvatarFileID, StatusText: u.StatusText,
	}
}

type Session struct {
	ID         string `json:"id"` // sha256 of the raw token
	UserID     string `json:"user_id"`
	CreatedAt  int64  `json:"created_at"`
	ExpiresAt  int64  `json:"expires_at"`
	LastSeenAt int64  `json:"last_seen_at"`
	UserAgent  string `json:"user_agent"`
	IP         string `json:"ip"`
}

const (
	FriendPending  = "pending"
	FriendAccepted = "accepted"
	FriendDeclined = "declined"
	FriendBlocked  = "blocked"
)

type Friendship struct {
	ID          string `json:"id"`
	RequesterID string `json:"requester_id"`
	AddresseeID string `json:"addressee_id"`
	Status      string `json:"status"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type Group struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IconFileID string `json:"icon_file_id,omitempty"`
	OwnerID    string `json:"owner_id"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

type GroupMember struct {
	GroupID  string `json:"group_id"`
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	JoinedAt int64  `json:"joined_at"`
}

const (
	ChannelGroupText = "group_text"
	ChannelDM        = "dm"
	// ChannelGroupVoice arrives in Phase 2; kind column has no CHECK so this is additive.
	ChannelGroupVoice = "group_voice"
)

type Channel struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	GroupID   string `json:"group_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Topic     string `json:"topic"`
	Position  int    `json:"position"`
	CreatedAt int64  `json:"created_at"`
	// DM only: the other participants (filled by store queries, not a column).
	Participants []PublicUser `json:"participants,omitempty"`
}

const (
	MessageDefault = "default"
	MessageSystem  = "system"
)

type Message struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	AuthorID  string `json:"author_id"`
	Kind      string `json:"kind"`
	Content   string `json:"content"`
	ReplyToID string `json:"reply_to_id,omitempty"`
	// MentionIDs lists users @mentioned in Content (channel members only).
	MentionIDs  []string     `json:"mention_ids,omitempty"`
	CreatedAt   int64        `json:"created_at"`
	EditedAt    int64        `json:"edited_at,omitempty"`
	DeletedAt   int64        `json:"deleted_at,omitempty"`
	Author      *PublicUser  `json:"author,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	ReplyTo     *Message     `json:"reply_to,omitempty"`
	// Nonce echoes the client-supplied idempotency key on create (not stored).
	Nonce string `json:"nonce,omitempty"`
}

type File struct {
	ID        string `json:"id"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	MIME      string `json:"mime"`
	CreatedAt int64  `json:"created_at"`
}

type Attachment struct {
	ID         string `json:"id"`
	FileID     string `json:"file_id"`
	MessageID  string `json:"message_id,omitempty"`
	UploaderID string `json:"uploader_id"`
	FileName   string `json:"file_name"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	// Denormalized from files for client convenience.
	MIME      string `json:"mime"`
	SizeBytes int64  `json:"size_bytes"`
}

const (
	InviteRegistration = "registration"
	InviteGroup        = "group"
)

type Invite struct {
	Code      string `json:"code"`
	Kind      string `json:"kind"`
	GroupID   string `json:"group_id,omitempty"`
	CreatedBy string `json:"created_by"`
	MaxUses   int64  `json:"max_uses,omitempty"` // 0 = unlimited
	UseCount  int64  `json:"use_count"`
	ExpiresAt int64  `json:"expires_at,omitempty"` // 0 = never
	RevokedAt int64  `json:"revoked_at,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// --- Phase 4: synchronized listening ---

type Playlist struct {
	ID         string `json:"id"`
	OwnerID    string `json:"owner_id"`
	ChannelID  string `json:"channel_id,omitempty"`
	Name       string `json:"name"`
	ArchivedAt int64  `json:"archived_at,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
	ItemCount  int    `json:"item_count"`
}

// PlaylistItem is one queue entry. Kind is "video" (YouTube) or "upload";
// Refs holds provider identifiers (youtube id, thumbnail, upload mime).
type PlaylistItem struct {
	ID           string            `json:"id"`
	PlaylistID   string            `json:"playlist_id"`
	Position     float64           `json:"position"`
	Kind         string            `json:"kind"`
	Title        string            `json:"title"`
	Artist       string            `json:"artist,omitempty"`
	DurationMS   int64             `json:"duration_ms,omitempty"`
	RefsJSON     string            `json:"-"`
	Refs         map[string]string `json:"refs"`
	AttachmentID string            `json:"attachment_id,omitempty"`
	AddedBy      string            `json:"added_by"`
	CreatedAt    int64             `json:"created_at"`
}

// PlaybackSession is the server-authoritative transport state. While
// playing, position = position_ms + (server_now - anchor_server_ms).
type PlaybackSession struct {
	ID             string `json:"id"`
	ChannelID      string `json:"channel_id"`
	HostUserID     string `json:"host_user_id"`
	PlaylistID     string `json:"playlist_id"`
	CurrentItemID  string `json:"current_item_id,omitempty"`
	State          string `json:"state"` // playing | paused | ended
	PositionMS     int64  `json:"position_ms"`
	AnchorServerMS int64  `json:"anchor_server_ms"`
	StartedAt      int64  `json:"started_at"`
	EndedAt        int64  `json:"ended_at,omitempty"`
}

type PushSubscription struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Endpoint  string `json:"endpoint"`
	P256dh    string `json:"p256dh"`
	Auth      string `json:"auth"`
	UserAgent string `json:"user_agent"`
	CreatedAt int64  `json:"created_at"`
}

type ReadState struct {
	UserID            string `json:"user_id,omitempty"`
	ChannelID         string `json:"channel_id"`
	LastReadMessageID string `json:"last_read_message_id,omitempty"`
	UpdatedAt         int64  `json:"updated_at"`
}
