// Shared types mirroring the server's internal/model JSON shapes.

export interface User {
  id: string
  username: string
  display_name: string
  avatar_file_id?: string
  status_text: string
  is_admin?: boolean
  created_at?: number
  disabled_at?: number
}

export interface Friendship {
  id: string
  requester_id: string
  addressee_id: string
  status: 'pending' | 'accepted' | 'declined' | 'blocked' | 'removed'
  created_at: number
  updated_at: number
}

export interface FriendEntry {
  friendship: Friendship
  user_id: string
}

export interface Group {
  id: string
  name: string
  icon_file_id?: string
  owner_id: string
  created_at: number
  updated_at: number
}

export interface GroupMember {
  group_id: string
  user_id: string
  role: 'owner' | 'admin' | 'member'
  joined_at: number
}

export interface Channel {
  id: string
  kind: 'group_text' | 'dm' | 'group_voice'
  group_id?: string
  name?: string
  topic: string
  position: number
  created_at: number
  participants?: User[]
}

export interface Attachment {
  id: string
  file_id: string
  message_id?: string
  uploader_id: string
  file_name: string
  width?: number
  height?: number
  duration_ms?: number
  created_at: number
  mime: string
  size_bytes: number
}

export interface Message {
  id: string
  channel_id: string
  author_id: string
  kind: 'default' | 'system'
  content: string
  reply_to_id?: string
  created_at: number
  edited_at?: number
  deleted_at?: number
  author?: User
  attachments?: Attachment[]
  reply_to?: Message
  nonce?: string
  // Client-only: set while an optimistic send is in flight / failed.
  pending?: boolean
  failed?: boolean
}

export interface ReadState {
  channel_id: string
  last_read_message_id?: string
  updated_at: number
}

export interface Invite {
  code: string
  kind: 'registration' | 'group'
  group_id?: string
  created_by: string
  max_uses?: number
  use_count: number
  expires_at?: number
  revoked_at?: number
  created_at: number
}

export interface GroupSummary {
  group: Group
  channels: Channel[]
  members: GroupMember[]
}

export interface ReadyPayload {
  user: User
  groups: GroupSummary[]
  dm_channels: Channel[]
  friends: FriendEntry[]
  read_states: ReadState[]
  latest_message_ids: Record<string, string>
  online_user_ids: string[] | null
  users: Record<string, User>
}

export interface SessionInfo {
  id: string
  created_at: number
  last_seen_at: number
  user_agent: string
  ip: string
  current: boolean
}

// Admin-editable server configuration (mirrors internal/api configPayload).
export interface ServerConfig {
  server: {
    name: string
    http_port: number
    max_upload_mb: number
    open_browser: boolean
  }
  tls: {
    mode: 'auto' | 'acme' | 'selfsigned' | 'off'
    domain: string
    acme_email: string
    https_port: number
  }
  ddns: {
    enabled: boolean
    provider: 'duckdns' | 'custom'
    domain: string
    token: string
    url: string
    interval_minutes: number
  }
  lan_dns: {
    enabled: boolean
    upstream: string
    answer_ip: string
  }
  webrtc: {
    udp_port: number
    public_ip: string
  }
}

export interface LanDNSStatus {
  active: boolean
  error: string
}
