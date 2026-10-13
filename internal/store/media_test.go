package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/skyleb/talaria/internal/model"
)

// mediaFixture seeds two groups, a DM, chat uploads of every type, a queue
// upload, and a deleted message, and returns the ids the tests assert on.
type mediaFixture struct {
	alice, bob *model.User
	g1Chan     string // alice + bob
	g2Chan     string // alice only
	dmChan     string // alice + bob
	// attachment ids by label
	att map[string]string
}

func seedMedia(t *testing.T, s *Store) mediaFixture {
	t.Helper()
	ctx := context.Background()
	alice, err := s.CreateUser(ctx, "alice", "Alice", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.CreateUser(ctx, "bob", "Bob", "x", false)
	if err != nil {
		t.Fatal(err)
	}
	_, g1, err := s.CreateGroup(ctx, "g1", alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	g1Group := g1.GroupID
	if err := s.AddGroupMember(ctx, g1Group, bob.ID, "member"); err != nil {
		t.Fatal(err)
	}
	_, g2, err := s.CreateGroup(ctx, "g2", alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	dm, _, err := s.GetOrCreateDM(ctx, alice.ID, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	f := mediaFixture{alice: alice, bob: bob, g1Chan: g1.ID, g2Chan: g2.ID, dmChan: dm.ID, att: map[string]string{}}

	upload := func(label, uploader, name, mime string, size int64) string {
		file := &model.File{SHA256: "sha-" + label, SizeBytes: size, MIME: mime}
		if err := s.CreateFile(ctx, file); err != nil {
			t.Fatal(err)
		}
		a := &model.Attachment{FileID: file.ID, UploaderID: uploader, FileName: name}
		if err := s.CreateAttachment(ctx, a); err != nil {
			t.Fatal(err)
		}
		f.att[label] = a.ID
		return a.ID
	}
	post := func(channel, author string, attIDs ...string) *model.Message {
		m := &model.Message{ChannelID: channel, AuthorID: author, Content: "hi"}
		if err := s.CreateMessage(ctx, m, attIDs); err != nil {
			t.Fatal(err)
		}
		return m
	}

	// g1: alice posts an image and a video; bob posts a pdf.
	post(f.g1Chan, alice.ID, upload("g1-img", alice.ID, "Beach.jpg", "image/jpeg", 300))
	post(f.g1Chan, alice.ID, upload("g1-vid", alice.ID, "clip.mp4", "video/mp4", 5000))
	post(f.g1Chan, bob.ID, upload("g1-pdf", bob.ID, "notes.pdf", "application/pdf", 100))
	// g1: a deleted message whose attachment must never show up.
	del := post(f.g1Chan, alice.ID, upload("g1-deleted", alice.ID, "gone.png", "image/png", 50))
	if err := s.SoftDeleteMessage(ctx, del.ID); err != nil {
		t.Fatal(err)
	}
	// g2 (alice only): one image.
	post(f.g2Chan, alice.ID, upload("g2-img", alice.ID, "alps.png", "image/png", 700))
	// dm: bob sends audio.
	post(f.dmChan, bob.ID, upload("dm-audio", bob.ID, "song.mp3", "audio/mpeg", 4000))
	// g1 listening queue: an upload with no message, in two playlists (live + saved).
	queueAtt := upload("g1-queue", alice.ID, "mix.mp3", "audio/mpeg", 9000)
	for _, name := range []string{"", "Saved"} {
		pl, err := s.CreatePlaylist(ctx, alice.ID, f.g1Chan, name)
		if err != nil {
			t.Fatal(err)
		}
		it := &model.PlaylistItem{PlaylistID: pl.ID, Kind: "upload", Title: "mix", AttachmentID: queueAtt, AddedBy: alice.ID,
			Refs: map[string]string{"upload": queueAtt}}
		if err := s.AppendPlaylistItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	// A pending upload (no message, no playlist) must not be listed.
	upload("pending", alice.ID, "draft.png", "image/png", 10)
	return f
}

func ids(items []MediaItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func TestListMediaScopesAndSources(t *testing.T) {
	s := testStore(t)
	f := seedMedia(t, s)
	ctx := context.Background()

	// Channel scope: g1 has image, video, pdf and the queue upload once.
	items, next, err := s.ListMedia(ctx, MediaQuery{ChannelIDs: []string{f.g1Chan}})
	if err != nil {
		t.Fatal(err)
	}
	if next != "" || len(items) != 4 {
		t.Fatalf("g1: got %d items (next=%q): %v", len(items), next, ids(items))
	}
	seen := map[string]MediaItem{}
	for _, it := range items {
		seen[it.ID] = it
		if it.ChannelID != f.g1Chan {
			t.Errorf("item %s channel %s, want g1", it.FileName, it.ChannelID)
		}
	}
	if _, ok := seen[f.att["g1-deleted"]]; ok {
		t.Error("deleted message's attachment was listed")
	}
	if q, ok := seen[f.att["g1-queue"]]; !ok || q.Source != "queue" {
		t.Errorf("queue upload missing or wrong source: %+v", q)
	}
	if seen[f.att["g1-img"]].Source != "message" {
		t.Error("message attachment should have source=message")
	}

	// Everything bob can see: g1 + dm, never g2 or the pending upload.
	bobChans, err := s.VisibleChannelIDs(ctx, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	items, _, err = s.ListMedia(ctx, MediaQuery{ChannelIDs: bobChans})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("bob sees %d items, want 5: %v", len(items), ids(items))
	}
	for _, it := range items {
		if it.ID == f.att["g2-img"] || it.ID == f.att["pending"] {
			t.Errorf("bob must not see %s", it.FileName)
		}
	}
}

func TestListMediaFilters(t *testing.T) {
	s := testStore(t)
	f := seedMedia(t, s)
	ctx := context.Background()
	all := []string{f.g1Chan, f.g2Chan, f.dmChan}

	cases := []struct {
		name string
		q    MediaQuery
		want []string
	}{
		{"images", MediaQuery{ChannelIDs: all, Type: "image"}, []string{"g2-img", "g1-img"}},
		{"videos", MediaQuery{ChannelIDs: all, Type: "video"}, []string{"g1-vid"}},
		{"audio", MediaQuery{ChannelIDs: all, Type: "audio"}, []string{"g1-queue", "dm-audio"}},
		{"files", MediaQuery{ChannelIDs: all, Type: "file"}, []string{"g1-pdf"}},
		{"uploader bob", MediaQuery{ChannelIDs: all, Uploader: f.bob.ID}, []string{"dm-audio", "g1-pdf"}},
		{"name search", MediaQuery{ChannelIDs: all, Query: "ALP"}, []string{"g2-img"}},
		{"name sort", MediaQuery{ChannelIDs: all, Sort: MediaSortName}, []string{"g2-img", "g1-img", "g1-vid", "g1-queue", "g1-pdf", "dm-audio"}},
		{"largest", MediaQuery{ChannelIDs: all, Sort: MediaSortLargest}, []string{"g1-queue", "g1-vid", "dm-audio", "g2-img", "g1-img", "g1-pdf"}},
	}
	for _, c := range cases {
		items, _, err := s.ListMedia(ctx, c.q)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		want := make([]string, len(c.want))
		for i, l := range c.want {
			want[i] = f.att[l]
		}
		if fmt.Sprint(ids(items)) != fmt.Sprint(want) {
			t.Errorf("%s: got %v want %v", c.name, names(items), c.want)
		}
	}
}

func names(items []MediaItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.FileName
	}
	return out
}

func TestListMediaPagingEverySort(t *testing.T) {
	s := testStore(t)
	f := seedMedia(t, s)
	ctx := context.Background()
	all := []string{f.g1Chan, f.g2Chan, f.dmChan}
	for _, sort := range []string{MediaSortNewest, MediaSortOldest, MediaSortLargest, MediaSortName} {
		full, next, err := s.ListMedia(ctx, MediaQuery{ChannelIDs: all, Sort: sort, Limit: 100})
		if err != nil || next != "" {
			t.Fatalf("%s: full page err=%v next=%q", sort, err, next)
		}
		var paged []string
		cursor := ""
		for pages := 0; pages < 10; pages++ {
			items, n, err := s.ListMedia(ctx, MediaQuery{ChannelIDs: all, Sort: sort, Limit: 2, Cursor: cursor})
			if err != nil {
				t.Fatalf("%s: page %d: %v", sort, pages, err)
			}
			paged = append(paged, ids(items)...)
			if n == "" {
				break
			}
			cursor = n
		}
		if fmt.Sprint(paged) != fmt.Sprint(ids(full)) {
			t.Errorf("%s: paged %v != full %v", sort, paged, ids(full))
		}
	}
	// A cursor from another sort order is rejected.
	_, next, _ := s.ListMedia(ctx, MediaQuery{ChannelIDs: all, Sort: MediaSortName, Limit: 1})
	if _, _, err := s.ListMedia(ctx, MediaQuery{ChannelIDs: all, Sort: MediaSortNewest, Cursor: next}); err != ErrBadCursor {
		t.Errorf("cross-sort cursor: got %v, want ErrBadCursor", err)
	}
}

func TestMediaUploaders(t *testing.T) {
	s := testStore(t)
	f := seedMedia(t, s)
	got, err := s.MediaUploaders(context.Background(), []string{f.g2Chan})
	if err != nil || len(got) != 1 || got[0] != f.alice.ID {
		t.Fatalf("g2 uploaders = %v, %v; want [alice]", got, err)
	}
	got, err = s.MediaUploaders(context.Background(), []string{f.g1Chan, f.dmChan})
	if err != nil || len(got) != 2 {
		t.Fatalf("g1+dm uploaders = %v, %v; want alice and bob", got, err)
	}
}
