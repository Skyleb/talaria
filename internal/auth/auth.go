// Package auth handles password hashing and cookie sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"

	"github.com/skyleb/talaria/internal/model"
	"github.com/skyleb/talaria/internal/store"
)

const (
	CookieName    = "talaria_session"
	sessionTTL    = 30 * 24 * time.Hour
	touchInterval = time.Hour // only rewrite last_seen/expiry this often
)

var hashParams = &argon2id.Params{
	Memory: 64 * 1024, Iterations: 2, Parallelism: 2, SaltLength: 16, KeyLength: 32,
}

func HashPassword(pw string) (string, error) {
	return argon2id.CreateHash(pw, hashParams)
}

func CheckPassword(pw, hash string) bool {
	ok, err := argon2id.ComparePasswordAndHash(pw, hash)
	return err == nil && ok
}

type Manager struct {
	store *store.Store
	// Secure marks cookies Secure; enable whenever TLS is on.
	Secure bool
}

func NewManager(s *store.Store) *Manager { return &Manager{store: s} }

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// Issue creates a session and sets the cookie on w.
func (m *Manager) Issue(ctx context.Context, w http.ResponseWriter, r *http.Request, userID string) error {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	nowMs := time.Now().UnixMilli()
	sess := &model.Session{
		ID: hashToken(tok), UserID: userID,
		CreatedAt: nowMs, LastSeenAt: nowMs,
		ExpiresAt: time.Now().Add(sessionTTL).UnixMilli(),
		UserAgent: r.UserAgent(), IP: ip,
	}
	if err := m.store.CreateSession(ctx, sess); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: tok, Path: "/",
		HttpOnly: true, Secure: m.Secure, SameSite: http.SameSiteLaxMode,
		MaxAge: int(sessionTTL.Seconds()),
	})
	return nil
}

func (m *Manager) Clear(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		m.store.DeleteSession(ctx, hashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: m.Secure, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

var ErrUnauthorized = errors.New("unauthorized")

// Resolve returns the user for the request's session cookie.
func (m *Manager) Resolve(ctx context.Context, r *http.Request) (*model.User, *model.Session, error) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return nil, nil, ErrUnauthorized
	}
	sess, err := m.store.SessionByID(ctx, hashToken(c.Value))
	if err != nil {
		return nil, nil, ErrUnauthorized
	}
	nowMs := time.Now().UnixMilli()
	if sess.ExpiresAt < nowMs {
		m.store.DeleteSession(ctx, sess.ID)
		return nil, nil, ErrUnauthorized
	}
	u, err := m.store.UserByID(ctx, sess.UserID)
	if err != nil || u.DisabledAt != 0 {
		return nil, nil, ErrUnauthorized
	}
	// Sliding expiry, throttled to one write per hour.
	if nowMs-sess.LastSeenAt > touchInterval.Milliseconds() {
		m.store.TouchSession(ctx, sess.ID, nowMs, time.Now().Add(sessionTTL).UnixMilli())
	}
	return u, sess, nil
}

type ctxKey int

const (
	userKey ctxKey = iota
	sessionKey
)

// Middleware authenticates the request and stashes user+session in context.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, sess, err := m.Resolve(r.Context(), r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"code":"unauthorized","message":"not signed in"}}`))
			return
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		ctx = context.WithValue(ctx, sessionKey, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func UserFrom(ctx context.Context) *model.User {
	u, _ := ctx.Value(userKey).(*model.User)
	return u
}

func SessionFrom(ctx context.Context) *model.Session {
	s, _ := ctx.Value(sessionKey).(*model.Session)
	return s
}

// ValidUsername enforces 2-32 chars of [a-z0-9_.].
func ValidUsername(u string) bool {
	u = strings.ToLower(strings.TrimSpace(u))
	if len(u) < 2 || len(u) > 32 {
		return false
	}
	for _, r := range u {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

func ValidPassword(p string) bool { return len(p) >= 8 && len(p) <= 256 }
