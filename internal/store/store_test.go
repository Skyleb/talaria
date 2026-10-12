package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/skyle/lantern/internal/model"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	// Real temp file, not :memory: — WAL behavior differs.
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrationsIdempotent(t *testing.T) {
	s := testStore(t)
	if err := s.migrate(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestMessagePaginationCursor(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "alice", "Alice", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	_, ch, err := s.CreateGroup(ctx, "g", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := 0; i < 120; i++ {
		m := &model.Message{ChannelID: ch.ID, AuthorID: u.ID, Kind: "default", Content: "m"}
		if err := s.CreateMessage(ctx, m, nil); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
	}
	page1, err := s.ListMessages(ctx, ch.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 50 || page1[0].ID != ids[119] {
		t.Fatalf("page1: len=%d first=%s want newest %s", len(page1), page1[0].ID, ids[119])
	}
	page2, err := s.ListMessages(ctx, ch.ID, page1[len(page1)-1].ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 50 || page2[0].ID != ids[69] {
		t.Fatalf("page2 wrong window: len=%d first=%s", len(page2), page2[0].ID)
	}
	// No overlap between pages.
	seen := map[string]bool{}
	for _, m := range append(page1, page2...) {
		if seen[m.ID] {
			t.Fatalf("duplicate %s across pages", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestDMDedupe(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, "a1", "", "x", false)
	b, _ := s.CreateUser(ctx, "b1", "", "x", false)
	c1, created1, err := s.GetOrCreateDM(ctx, a.ID, b.ID)
	if err != nil || !created1 {
		t.Fatalf("first: created=%v err=%v", created1, err)
	}
	c2, created2, err := s.GetOrCreateDM(ctx, b.ID, a.ID) // reversed order
	if err != nil || created2 {
		t.Fatalf("second: created=%v err=%v", created2, err)
	}
	if c1.ID != c2.ID {
		t.Fatalf("dm not deduped: %s vs %s", c1.ID, c2.ID)
	}
}

func TestInviteConsumption(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "owner", "", "x", true)
	inv, err := s.CreateInvite(ctx, model.InviteRegistration, "", u.ID, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.ConsumeInvite(ctx, inv.Code); err != nil {
			t.Fatalf("use %d: %v", i+1, err)
		}
	}
	if _, err := s.ConsumeInvite(ctx, inv.Code); err != ErrInviteInvalid {
		t.Fatalf("third use should fail, got %v", err)
	}
	if err := s.RevokeInvite(ctx, inv.Code); err != nil {
		t.Fatal(err)
	}
}

func TestFriendshipPairUnique(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, "a2", "", "x", false)
	b, _ := s.CreateUser(ctx, "b2", "", "x", false)
	if _, err := s.CreateFriendRequest(ctx, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFriendRequest(ctx, b.ID, a.ID); err == nil {
		t.Fatal("reverse-direction duplicate pair should violate pair_key unique")
	}
	f, err := s.FriendshipBetween(ctx, b.ID, a.ID)
	if err != nil || f.Status != model.FriendPending {
		t.Fatalf("lookup both directions: %v %v", f, err)
	}
}

func TestOrphanAttachmentGC(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "up", "", "x", false)
	f := &model.File{SHA256: "deadbeef", SizeBytes: 10, MIME: "text/plain"}
	if err := s.CreateFile(ctx, f); err != nil {
		t.Fatal(err)
	}
	att := &model.Attachment{FileID: f.ID, UploaderID: u.ID, FileName: "x.txt"}
	if err := s.CreateAttachment(ctx, att); err != nil {
		t.Fatal(err)
	}
	// Cutoff in the future: the pending attachment is stale.
	shas, err := s.DeleteStaleOrphanAttachments(ctx, now()+1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(shas) != 1 || shas[0] != "deadbeef" {
		t.Fatalf("expected orphan blob deadbeef, got %v", shas)
	}
	if _, err := s.AttachmentByID(ctx, att.ID); err != ErrNotFound {
		t.Fatalf("attachment should be gone, got %v", err)
	}
}
