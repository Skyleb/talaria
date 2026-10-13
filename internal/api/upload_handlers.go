package api

import (
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/gabriel-vasile/mimetype"

	"github.com/skyleb/talaria/internal/auth"
	"github.com/skyleb/talaria/internal/model"
)

// MIME types allowed to render inline (everything else downloads as attachment).
func inlineAllowed(mime string) bool {
	if strings.HasPrefix(mime, "audio/") || strings.HasPrefix(mime, "video/") {
		return true
	}
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/avif", "application/pdf":
		return true
	}
	return false
}

var unsafeFilenameChars = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

func sanitizeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = unsafeFilenameChars.ReplaceAllString(name, "_")
	name = strings.Trim(name, ". ")
	// Windows reserved device names.
	upper := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	switch upper {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}
	if name == "" {
		name = "file"
	}
	if len(name) > 180 {
		name = name[len(name)-180:]
	}
	return name
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	maxBytes := s.Config.Server.MaxUploadMB * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(1<<20))

	mr, err := r.MultipartReader()
	if err != nil {
		badRequest(w, "expected multipart/form-data")
		return
	}
	var part *multipartFilePart
	durationMS := int64(0)
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			badRequest(w, "malformed multipart body")
			return
		}
		switch p.FormName() {
		case "duration_ms": // optional, client-reported best effort
			buf := make([]byte, 20)
			n, _ := p.Read(buf)
			durationMS, _ = strconv.ParseInt(strings.TrimSpace(string(buf[:n])), 10, 64)
		case "file":
			part = &multipartFilePart{reader: p, filename: p.FileName()}
		}
		if part != nil {
			break // file must be the last field we care about; stream it now
		}
	}
	if part == nil {
		badRequest(w, "missing 'file' field")
		return
	}

	sha, size, err := s.Blobs.Write(part.reader)
	if err != nil {
		if strings.Contains(err.Error(), "http: request body too large") {
			writeErr(w, 413, "too_large", "file exceeds the upload limit")
			return
		}
		serverError(w, s.Log, err)
		return
	}
	if size == 0 {
		badRequest(w, "empty file")
		return
	}

	// Sniff real content type from the stored blob; never trust the client.
	f, err := s.Blobs.Open(sha)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	mt, err := mimetype.DetectReader(f)
	if err != nil {
		f.Close()
		serverError(w, s.Log, err)
		return
	}
	mime := mt.String()
	if i := strings.Index(mime, ";"); i > 0 {
		mime = mime[:i]
	}

	// Image dimensions (best effort).
	width, height := 0, 0
	if strings.HasPrefix(mime, "image/") {
		if _, err := f.Seek(0, io.SeekStart); err == nil {
			if cfg, _, err := image.DecodeConfig(f); err == nil {
				width, height = cfg.Width, cfg.Height
			}
		}
	}
	f.Close()

	// Reuse an existing file row with the same hash, else create one.
	fileRec, err := s.Store.FileBySHA(r.Context(), sha)
	if err != nil {
		fileRec = &model.File{SHA256: sha, SizeBytes: size, MIME: mime}
		if err := s.Store.CreateFile(r.Context(), fileRec); err != nil {
			serverError(w, s.Log, err)
			return
		}
	}
	att := &model.Attachment{
		FileID: fileRec.ID, UploaderID: me.ID,
		FileName: sanitizeFilename(part.filename),
		Width:    width, Height: height, DurationMS: durationMS,
		MIME: fileRec.MIME, SizeBytes: fileRec.SizeBytes,
	}
	if err := s.Store.CreateAttachment(r.Context(), att); err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 201, map[string]any{"attachment": att})
}

type multipartFilePart struct {
	reader   io.Reader
	filename string
}

func (s *Server) handleServeFile(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	att, err := s.Store.AttachmentByID(r.Context(), r.PathValue("attachmentId"))
	if err != nil {
		notFound(w)
		return
	}
	// Access: uploader always; otherwise member of the message's channel.
	if att.UploaderID != me.ID {
		channelID := ""
		if att.MessageID != "" {
			m, err := s.Store.MessageByID(r.Context(), att.MessageID)
			if err != nil {
				notFound(w)
				return
			}
			channelID = m.ChannelID
		} else if cid, ok := s.Store.PlaylistChannelForAttachment(r.Context(), att.ID); ok {
			// A listening-queue upload: members of that chat may stream it.
			channelID = cid
		}
		if channelID == "" {
			notFound(w)
			return
		}
		ch, err := s.Store.ChannelByID(r.Context(), channelID)
		if err != nil {
			notFound(w)
			return
		}
		ok, err := s.Store.IsChannelMember(r.Context(), ch, me.ID)
		if err != nil || !ok {
			notFound(w)
			return
		}
	}
	fileRec, err := s.Store.FileByID(r.Context(), att.FileID)
	if err != nil {
		notFound(w)
		return
	}
	blobFile, err := s.Blobs.Open(fileRec.SHA256)
	if err != nil {
		notFound(w)
		return
	}
	defer blobFile.Close()

	w.Header().Set("Content-Type", fileRec.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Immutable: attachment content never changes under the same id.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if inlineAllowed(fileRec.MIME) {
		w.Header().Set("Content-Disposition", `inline; filename="`+att.FileName+`"`)
	} else {
		w.Header().Set("Content-Disposition", `attachment; filename="`+att.FileName+`"`)
		// Defense in depth for HTML/SVG/etc. that a browser might still render.
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	// ServeContent gives us Range requests (video/audio seeking) for free.
	http.ServeContent(w, r, "", time.UnixMilli(fileRec.CreatedAt), blobFile)
}

// handleServeAvatar serves (and lazily resizes) avatar/icon images by file id.
// Any authenticated user may fetch avatars — they are profile-public.
func (s *Server) handleServeAvatar(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("fileId")
	fileRec, err := s.Store.FileByID(r.Context(), fileID)
	if err != nil || !strings.HasPrefix(fileRec.MIME, "image/") {
		notFound(w)
		return
	}
	size := 128
	if v := r.URL.Query().Get("s"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 16 && n <= 512 {
			size = n
		}
	}
	cacheDir := filepath.Join(s.Config.ResolvedDataDir, "avatarcache")
	cachePath := filepath.Join(cacheDir, fileRec.SHA256+"-"+strconv.Itoa(size)+".png")
	if f, err := os.Open(cachePath); err == nil {
		defer f.Close()
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeContent(w, r, "", time.UnixMilli(fileRec.CreatedAt), f)
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
		notFound(w)
		return
	}
	thumb := imaging.Fill(img, size, size, imaging.Center, imaging.Lanczos)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		serverError(w, s.Log, err)
		return
	}
	if err := imaging.Save(thumb, cachePath); err != nil {
		serverError(w, s.Log, err)
		return
	}
	f, err := os.Open(cachePath)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, "", time.UnixMilli(fileRec.CreatedAt), f)
}

// StartAttachmentGC prunes pending attachments older than 24h and their blobs.
func (s *Server) StartAttachmentGC() {
	go func() {
		t := time.NewTicker(time.Hour)
		for range t.C {
			ctx, cancel := timeoutCtx()
			shas, err := s.Store.DeleteStaleOrphanAttachments(ctx, nowMs()-24*3600*1000)
			cancel()
			if err != nil {
				s.Log.Error("attachment GC", "err", err)
				continue
			}
			for _, sha := range shas {
				if err := s.Blobs.Remove(sha); err != nil {
					s.Log.Error("blob GC", "sha", sha, "err", err)
				}
			}
		}
	}()
}
