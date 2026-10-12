package api

import (
	"net/http"

	"github.com/skyle/lantern/internal/auth"
	"github.com/skyle/lantern/internal/web"
)

// Router assembles the full HTTP surface.
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	// --- public (no auth) ---
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
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

	authed.HandleFunc("POST /api/v1/uploads", s.handleUpload)

	authed.HandleFunc("GET /api/v1/admin/users", s.adminOnly(s.handleAdminListUsers))
	authed.HandleFunc("PATCH /api/v1/admin/users/{id}", s.adminOnly(s.handleAdminUpdateUser))
	authed.HandleFunc("GET /api/v1/admin/invites", s.adminOnly(s.handleAdminListInvites))
	authed.HandleFunc("POST /api/v1/admin/invites", s.adminOnly(s.handleAdminCreateInvite))
	authed.HandleFunc("DELETE /api/v1/admin/invites/{code}", s.adminOnly(s.handleAdminRevokeInvite))
	authed.HandleFunc("GET /api/v1/admin/stats", s.adminOnly(s.handleAdminStats))

	authed.HandleFunc("GET /api/v1/gateway", s.handleGateway)

	// File serving authenticates via cookie but lives outside /api for clean URLs.
	authed.HandleFunc("GET /files/{attachmentId}/{filename}", s.handleServeFile)
	authed.HandleFunc("GET /avatars/{fileId}", s.handleServeAvatar)

	mux.Handle("/api/v1/", s.Auth.Middleware(authed))
	mux.Handle("/files/", s.Auth.Middleware(authed))
	mux.Handle("/avatars/", s.Auth.Middleware(authed))

	// Embedded SPA with index.html fallback for client-side routes.
	mux.Handle("/", web.SPAHandler())

	return securityHeaders(mux)
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
