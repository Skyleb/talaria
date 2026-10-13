package api

import (
	"net/http"

	"github.com/skyleb/talaria/internal/auth"
	"github.com/skyleb/talaria/internal/web"
)

// Router assembles the full HTTP surface.
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	// --- public (no auth) ---
	// healthz allows cross-origin reads: the lan-access probe page fetches it
	// from the main domain's origin to test reachability.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		writeJSON(w, 200, map[string]string{"status": "ok", "version": s.Version})
	})
	mux.HandleFunc("GET /api/v1/setup/status", s.handleSetupStatus)
	mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
	mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("GET /api/v1/invites/{code}", s.handleInvitePreview)

	// --- authenticated ---
	authed := http.NewServeMux()
	authed.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	authed.HandleFunc("GET /api/v1/auth/sessions", s.handleListSessions)
	authed.HandleFunc("DELETE /api/v1/auth/sessions/{id}", s.handleDeleteSession)

	authed.HandleFunc("GET /api/v1/users/me", s.handleMe)
	authed.HandleFunc("GET /api/v1/server/info", s.handleServerInfo)
	authed.HandleFunc("PATCH /api/v1/users/me", s.handleUpdateMe)
	authed.HandleFunc("GET /api/v1/users/{id}", s.handleGetUser)

	authed.HandleFunc("GET /api/v1/friends", s.handleListFriends)
	authed.HandleFunc("POST /api/v1/friends/requests", s.handleFriendRequest)
	authed.HandleFunc("POST /api/v1/friends/requests/{id}/accept", s.handleFriendAccept)
	authed.HandleFunc("POST /api/v1/friends/requests/{id}/decline", s.handleFriendDecline)
	authed.HandleFunc("DELETE /api/v1/friends/{userId}", s.handleUnfriend)
	authed.HandleFunc("POST /api/v1/friends/{userId}/block", s.handleBlock)
	authed.HandleFunc("DELETE /api/v1/friends/{userId}/block", s.handleUnblock)

	authed.HandleFunc("POST /api/v1/groups", s.handleCreateGroup)
	authed.HandleFunc("GET /api/v1/groups/{id}", s.handleGetGroup)
	authed.HandleFunc("PATCH /api/v1/groups/{id}", s.handleUpdateGroup)
	authed.HandleFunc("DELETE /api/v1/groups/{id}", s.handleDeleteGroup)
	authed.HandleFunc("POST /api/v1/groups/{id}/channels", s.handleCreateChannel)
	authed.HandleFunc("POST /api/v1/groups/{id}/invites", s.handleCreateGroupInvite)
	authed.HandleFunc("DELETE /api/v1/groups/{id}/members/{userId}", s.handleRemoveMember)
	authed.HandleFunc("POST /api/v1/invites/{code}/join", s.handleJoinInvite)

	authed.HandleFunc("PATCH /api/v1/channels/{id}", s.handleUpdateChannel)
	authed.HandleFunc("DELETE /api/v1/channels/{id}", s.handleDeleteChannel)
	authed.HandleFunc("GET /api/v1/channels/{id}/messages", s.handleListMessages)
	authed.HandleFunc("POST /api/v1/channels/{id}/messages", s.handleCreateMessage)
	authed.HandleFunc("PUT /api/v1/channels/{id}/read", s.handleReadUpdate)
	authed.HandleFunc("PATCH /api/v1/messages/{id}", s.handleEditMessage)
	authed.HandleFunc("DELETE /api/v1/messages/{id}", s.handleDeleteMessage)

	authed.HandleFunc("POST /api/v1/dms", s.handleOpenDM)
	authed.HandleFunc("GET /api/v1/channels/{id}/listen", s.handleListenState)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen", s.handleListenStart)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen/join", s.handleListenJoin)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen/leave", s.handleListenLeave)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen/end", s.handleListenEnd)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen/play", s.handleListenPlay)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen/pause", s.handleListenPause)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen/seek", s.handleListenSeek)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen/select", s.handleListenSelect)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen/next", s.handleListenNext)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen/previous", s.handleListenPrevious)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen/items", s.handleListenAddItem)
	authed.HandleFunc("DELETE /api/v1/channels/{id}/listen/items/{itemId}", s.handleListenRemoveItem)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen/items/{itemId}/duration", s.handleListenItemDuration)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen/host", s.handleListenHost)
	authed.HandleFunc("POST /api/v1/channels/{id}/listen/save", s.handleListenSave)
	authed.HandleFunc("GET /api/v1/channels/{id}/playlists", s.handleListPlaylists)
	authed.HandleFunc("GET /api/v1/listen/search", s.handleListenSearch)
	authed.HandleFunc("GET /api/v1/gifs/status", s.handleGifStatus)
	authed.HandleFunc("GET /api/v1/search", s.handleSearch)
	authed.HandleFunc("GET /api/v1/media", s.handleListMedia)
	authed.HandleFunc("GET /api/v1/media/uploaders", s.handleMediaUploaders)
	authed.HandleFunc("DELETE /api/v1/playlists/{id}", s.handleDeletePlaylist)
	authed.HandleFunc("PUT /api/v1/channels/{id}/mute", s.handleMuteChannel)
	authed.HandleFunc("DELETE /api/v1/channels/{id}/mute", s.handleUnmuteChannel)
	authed.HandleFunc("GET /api/v1/push/vapid", s.handlePushVAPID)
	authed.HandleFunc("GET /api/v1/push/status", s.handlePushStatus)
	authed.HandleFunc("POST /api/v1/push/subscriptions", s.handlePushSubscribe)
	authed.HandleFunc("DELETE /api/v1/push/subscriptions", s.handlePushUnsubscribe)
	authed.HandleFunc("POST /api/v1/channels/{id}/participants", s.handleAddDMParticipant)
	authed.HandleFunc("DELETE /api/v1/channels/{id}/participants/me", s.handleLeaveDM)

	authed.HandleFunc("POST /api/v1/uploads", s.handleUpload)

	authed.HandleFunc("GET /api/v1/admin/users", s.adminOnly(s.handleAdminListUsers))
	authed.HandleFunc("PATCH /api/v1/admin/users/{id}", s.adminOnly(s.handleAdminUpdateUser))
	authed.HandleFunc("GET /api/v1/admin/invites", s.adminOnly(s.handleAdminListInvites))
	authed.HandleFunc("POST /api/v1/admin/invites", s.adminOnly(s.handleAdminCreateInvite))
	authed.HandleFunc("DELETE /api/v1/admin/invites/{code}", s.adminOnly(s.handleAdminRevokeInvite))
	authed.HandleFunc("GET /api/v1/admin/stats", s.adminOnly(s.handleAdminStats))
	authed.HandleFunc("GET /api/v1/admin/config", s.adminOnly(s.handleAdminGetConfig))
	authed.HandleFunc("PUT /api/v1/admin/config", s.adminOnly(s.handleAdminPutConfig))

	authed.HandleFunc("GET /api/v1/gateway", s.handleGateway)

	// File serving authenticates via cookie but lives outside /api for clean URLs.
	authed.HandleFunc("GET /files/{attachmentId}/{filename}", s.handleServeFile)
	authed.HandleFunc("GET /thumbs/{attachmentId}", s.handleServeThumb)
	authed.HandleFunc("GET /avatars/{fileId}", s.handleServeAvatar)

	mux.Handle("/api/v1/", s.Auth.Middleware(authed))
	mux.Handle("/files/", s.Auth.Middleware(authed))
	mux.Handle("/thumbs/", s.Auth.Middleware(authed))
	mux.Handle("/avatars/", s.Auth.Middleware(authed))

	// Embedded SPA with index.html fallback for client-side routes.
	mux.Handle("/", web.SPAHandler())

	return securityHeaders(s.lanRedirect(mux))
}

func (s *Server) adminOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if u := auth.UserFrom(r.Context()); u == nil || !u.IsAdmin {
			forbidden(w, "admin only")
			return
		}
		h(w, r)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleGateway(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	s.Hub.ServeWS(w, r, u.ID)
}
