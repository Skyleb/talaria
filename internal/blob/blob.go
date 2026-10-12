// Package blob is a content-addressed file store: data/blobs/<sha[0:2]>/<sha>.
package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Store struct {
	root string // <data>/blobs
	tmp  string // <data>/tmp — same volume so rename is atomic
}

func New(dataDir string) (*Store, error) {
	s := &Store{root: filepath.Join(dataDir, "blobs"), tmp: filepath.Join(dataDir, "tmp")}
	for _, d := range []string{s.root, s.tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) pathFor(sha string) string {
	return filepath.Join(s.root, sha[:2], sha)
}

// Write streams r to disk (never buffering in RAM), hashing along the way.
// Returns the hex sha256 and byte count. Duplicate content is discarded.
func (s *Store) Write(r io.Reader) (sha string, size int64, err error) {
	tmp, err := os.CreateTemp(s.tmp, "upload-*")
	if err != nil {
		return "", 0, err
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName) // no-op after successful rename
	}()

	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	sha = hex.EncodeToString(h.Sum(nil))
	dest := s.pathFor(sha)
	if _, err := os.Stat(dest); err == nil {
		return sha, size, nil // dedup: blob already present
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return "", 0, err
	}
	return sha, size, nil
}

// Open returns a ReadSeeker for the blob (http.ServeContent needs Seek for Range).
func (s *Store) Open(sha string) (*os.File, error) {
	if len(sha) < 3 {
		return nil, errors.New("bad sha")
	}
	return os.Open(s.pathFor(sha))
}

func (s *Store) Remove(sha string) error {
	if len(sha) < 3 {
		return errors.New("bad sha")
	}
	err := os.Remove(s.pathFor(sha))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Usage walks the blob tree and reports total bytes (admin stats).
func (s *Store) Usage() (int64, error) {
	var total int64
	err := filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("blob usage: %w", err)
	}
	return total, nil
}
