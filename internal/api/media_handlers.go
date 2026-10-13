package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/disintegration/imaging"

	"github.com/skyleb/talaria/internal/auth"
	"github.com/skyleb/talaria/internal/model"
	"github.com/skyleb/talaria/internal/store"
)

// mediaScopeChannels resolves ?scope=channel|group|all (+id) to the channel
// ids the caller may read, writing the error response itself on failure.
// Same rules as search.
func (s *Server) mediaScopeChannels(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	me := auth.UserFrom(r.Context())
	q := r.URL.Query()
	switch q.Get("scope") {
	case "channel":
		ch, ok := s.loadChannelAsMember(w, r, q.Get("id"))
		if !ok {
			return nil, false
		}
		return []string{ch.ID}, true
	case "group":
		g, _, ok := s.requireGroupRole(w, r, q.Get("id"))
		if !ok {
			return nil, false
		}
		chans, err := s.Store.ListChannelsByGroup(r.Context(), g.ID)
		if err != nil {
			serverError(w, s.Log, err)
			return nil, false
		}
		ids := make([]string, 0, len(chans))
		for _, c := range chans {
			ids = append(ids, c.ID)
		}
		return ids, true
	default:
		ids, err := s.Store.VisibleChannelIDs(r.Context(), me.ID)
		if err != nil {
			serverError(w, s.Log, err)
			return nil, false
		}
		return ids, true
	}
}

// handleListMedia is one page of the media view.
// GET /api/v1/media?scope=&id=&type=&uploader=&q=&sort=&cursor=
func (s *Server) handleListMedia(w http.ResponseWriter, r *http.Request) {
	channelIDs, ok := s.mediaScopeChannels(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	typ := q.Get("type")
	switch typ {
	case "", "image", "video", "audio", "file":
	default:
		badRequest(w, "type must be image, video, audio or file")
		return
	}
	sortBy := q.Get("sort")
	switch sortBy {
	case "", store.MediaSortNewest, store.MediaSortOldest, store.MediaSortLargest, store.MediaSortName:
	default:
		badRequest(w, "sort must be newest, oldest, largest or name")
		return
	}
	needle := strings.TrimSpace(q.Get("q"))
	if len(needle) > 100 {
		needle = needle[:100]
	}
	items, next, err := s.Store.ListMedia(r.Context(), store.MediaQuery{
		ChannelIDs: channelIDs,
		Type:       typ,
		Uploader:   q.Get("uploader"),
		Query:      needle,
		Sort:       sortBy,
		Cursor:     q.Get("cursor"),
		Limit:      60,
	})
	if err != nil {
		if errors.Is(err, store.ErrBadCursor) {
			badRequest(w, "invalid cursor")
			return
		}
		serverError(w, s.Log, err)
		return
	}
	idSet := map[string]struct{}{}
	for _, it := range items {
		idSet[it.UploaderID] = struct{}{}
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	users, err := s.Store.PublicUsersByIDs(r.Context(), ids)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	if users == nil {
		users = map[string]model.PublicUser{}
	}
	writeJSON(w, 200, map[string]any{
		"items":       items,
		"next_cursor": next,
		"has_more":    next != "",
		"users":       users,
	})
}

// handleMediaUploaders lists everyone who has shared media in the scope, for
// the uploader filter. GET /api/v1/media/uploaders?scope=&id=
func (s *Server) handleMediaUploaders(w http.ResponseWriter, r *http.Request) {
	channelIDs, ok := s.mediaScopeChannels(w, r)
	if !ok {
		return
	}
	ids, err := s.Store.MediaUploaders(r.Context(), channelIDs)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	users, err := s.Store.PublicUsersByIDs(r.Context(), ids)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	list := make([]model.PublicUser, 0, len(users))
	for _, u := range users {
		list = append(list, u)
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i].DisplayName, list[j].DisplayName
		if a == "" {
			a = list[i].Username
		}
		if b == "" {
			b = list[j].Username
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	writeJSON(w, 200, map[string]any{"users": list})
}

// handleServeThumb serves an aspect-preserving thumbnail of an image
// attachment, with the same access rule as the file itself.
// GET /thumbs/{attachmentId}?s=64..512 (default 256)
func (s *Server) handleServeThumb(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	att, err := s.Store.AttachmentByID(r.Context(), r.PathValue("attachmentId"))
	if err != nil || !s.canAccessAttachment(r.Context(), me.ID, att) {
		notFound(w)
		return
	}
	fileRec, err := s.Store.FileByID(r.Context(), att.FileID)
	if err != nil || !strings.HasPrefix(fileRec.MIME, "image/") {
		notFound(w)
		return
	}
	size := 256
	if v := r.URL.Query().Get("s"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			size = min(512, max(64, n))
		}
	}
	// Sources that may carry transparency stay PNG; photos go JPEG.
	ext, ctype := "jpg", "image/jpeg"
	switch fileRec.MIME {
	case "image/png", "image/gif", "image/webp":
		ext, ctype = "png", "image/png"
	}
	cacheDir := filepath.Join(s.Config.ResolvedDataDir, "thumbcache")
	cachePath := filepath.Join(cacheDir, fileRec.SHA256+"-fit"+strconv.Itoa(size)+"."+ext)
	serve := func(f *os.File) {
		defer f.Close()
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		http.ServeContent(w, r, "", time.UnixMilli(fileRec.CreatedAt), f)
	}
	if f, err := os.Open(cachePath); err == nil {
		serve(f)
		return
	}
	src, err := s.Blobs.Open(fileRec.SHA256)
	if err != nil {
		notFound(w)
		return
	}
	img, err := imaging.Decode(src, imaging.AutoOrientation(true))
	src.Close()
	if err != nil {
		// Formats imaging can't decode (e.g. webp/avif): the client falls
		// back to the original file.
		notFound(w)
		return
	}
	thumb := imaging.Fit(img, size, size, imaging.Lanczos)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		serverError(w, s.Log, err)
		return
	}
	if err := imaging.Save(thumb, cachePath, imaging.JPEGQuality(82)); err != nil {
		serverError(w, s.Log, err)
		return
	}
	f, err := os.Open(cachePath)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	serve(f)
}
