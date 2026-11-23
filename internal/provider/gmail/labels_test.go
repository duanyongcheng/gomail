package gmail_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/provider/gmail"
	"github.com/duanyongcheng/gomail/internal/store"
	"github.com/duanyongcheng/gomail/internal/testgmail"
)

func labelFixture(t *testing.T) (*gmail.Provider, *testgmail.Mock, *store.DB) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p, mock, _ := fixture(t, db, "a")
	mock.ProfileEmail = "a@example.invalid"
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	return p, mock, db
}
func labelWrites(m *testgmail.Mock) int { m.Lock(); defer m.Unlock(); return m.LabelWrites }
func TestGmailLabelCRUDKeepsStableFolderIDAndMessageData(t *testing.T) {
	p, mock, db := labelFixture(t)
	ctx := t.Context()
	if err := p.ChangeLabel(ctx, domain.LabelChange{Action: "create", Name: "  项目/工作  "}); err != nil {
		t.Fatal(err)
	}
	folders, _ := db.Folders(ctx, "a")
	var created domain.Folder
	for _, f := range folders {
		if f.Name == "项目/工作" {
			created = f
		}
	}
	if created.ID == "" || created.IsSystem {
		t.Fatal(folders)
	}
	if err := p.ChangeLabel(ctx, domain.LabelChange{Action: "rename", FolderID: created.ID, Name: "Renamed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.QueueMailMutation(ctx, store.MailMutation{MessageIDs: []string{"a:m1"}, OpType: "move_to_folder", TargetFolderID: created.ID}); err != nil {
		t.Fatal(err)
	}
	if err := p.ChangeLabel(ctx, domain.LabelChange{Action: "delete", FolderID: created.ID}); err == nil {
		t.Fatal("deleted target of pending operation")
	}
	ops, payloads, err := db.ClaimPendingOps(ctx, 1, time.Now().Unix())
	if err != nil || len(ops) != 1 {
		t.Fatal(ops, err)
	}
	if _, err := p.Replay(ctx, ops[0].OpType, payloads[ops[0].ID]); err != nil {
		t.Fatal(err)
	}
	if err := db.CompletePendingOp(ctx, ops[0].ID); err != nil {
		t.Fatal(err)
	}
	attachments, err := db.Attachments(ctx, "a:m1")
	if err != nil || len(attachments) == 0 {
		t.Fatal(attachments, err)
	}
	if err := p.ChangeLabel(ctx, domain.LabelChange{Action: "delete", FolderID: created.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Folder(ctx, created.ID); err == nil {
		t.Fatal("deleted label retained")
	}
	if _, err := db.Message(ctx, "a:m1"); err != nil {
		t.Fatal("label deletion deleted message", err)
	}
	if after, err := db.Attachments(ctx, "a:m1"); err != nil || len(after) != len(attachments) {
		t.Fatal(after, err)
	}
	if _, err := p.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Folder(ctx, created.ID); err == nil {
		t.Fatal("sync resurrected label")
	}
	if labelWrites(mock) != 3 {
		t.Fatal(labelWrites(mock))
	}
}
func TestGmailLabelRejectsSystemNamesOwnershipAndMailboxMismatch(t *testing.T) {
	p, mock, db := labelFixture(t)
	ctx := t.Context()
	for _, change := range []domain.LabelChange{{Action: "create", Name: "INBOX"}, {Action: "create", Name: "\n"}, {Action: "rename", FolderID: "a:label:INBOX", Name: "Changed"}, {Action: "delete", FolderID: "a:label:INBOX"}, {Action: "delete", FolderID: "other:label:Label_work"}, {Action: "create", Name: "工作"}} {
		if err := p.ChangeLabel(ctx, change); err == nil {
			t.Fatal("invalid label change accepted", change)
		}
	}
	mock.Lock()
	mock.ProfileEmail = "wrong@example.invalid"
	mock.Unlock()
	if err := p.ChangeLabel(ctx, domain.LabelChange{Action: "create", Name: "Fresh"}); err == nil {
		t.Fatal("wrong mailbox accepted")
	}
	if pending, err := db.GmailLabelChange(ctx, "a"); err != nil || pending != nil {
		t.Fatal(pending, err)
	}
	if labelWrites(mock) != 0 {
		t.Fatal("invalid operation called write endpoint")
	}
}
func TestLostLabelResponseReconcilesAfterRestartWithoutReplaying(t *testing.T) {
	for _, action := range []string{"create", "rename", "delete"} {
		t.Run(action, func(t *testing.T) {
			p, mock, db := labelFixture(t)
			ctx := t.Context()
			change := domain.LabelChange{Action: action, FolderID: "a:label:Label_work", Name: "New label"}
			mock.Lock()
			mock.LoseLabelResponse = true
			mock.Unlock()
			if err := p.ChangeLabel(ctx, change); err == nil {
				t.Fatal("lost response reported success")
			}
			pending, err := db.GmailLabelChange(ctx, "a")
			if err != nil || pending == nil {
				t.Fatal(pending, err)
			}
			if err := p.ChangeLabel(ctx, change); err == nil || labelWrites(mock) != 1 {
				t.Fatal("uncertain action replayed", err, labelWrites(mock))
			}
			path := db.Path()
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := store.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			provider := gmail.New(reopened, mock.API(&tokens{}), "a")
			if err := provider.RefreshLabels(ctx); err != nil {
				t.Fatal(err)
			}
			pending, err = reopened.GmailLabelChange(ctx, "a")
			if err != nil || pending != nil || labelWrites(mock) != 1 {
				t.Fatal(pending, err, labelWrites(mock))
			}
		})
	}
}
func TestDefiniteLabelRejectionClearsIntentButDBFailureKeepsIt(t *testing.T) {
	p, mock, db := labelFixture(t)
	ctx := t.Context()
	mock.Lock()
	mock.LabelWriteStatus = 403
	mock.Unlock()
	if err := p.ChangeLabel(ctx, domain.LabelChange{Action: "create", Name: "Rejected"}); err == nil {
		t.Fatal("403 accepted")
	}
	if pending, err := db.GmailLabelChange(ctx, "a"); err != nil || pending != nil {
		t.Fatal(pending, err)
	}
	mock.Lock()
	mock.LabelWriteStatus = 0
	mock.Unlock()
	conn, err := sql.Open("sqlite", db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`CREATE TRIGGER fail_label_cache BEFORE INSERT ON folders WHEN NEW.name='Accepted remotely' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := p.ChangeLabel(ctx, domain.LabelChange{Action: "create", Name: "Accepted remotely"}); err == nil {
		t.Fatal("DB failure hidden")
	}
	if pending, _ := db.GmailLabelChange(ctx, "a"); pending == nil {
		t.Fatal("intent lost after remote success")
	}
	if _, err := conn.Exec("DROP TRIGGER fail_label_cache"); err != nil {
		t.Fatal(err)
	}
	if err := p.RefreshLabels(ctx); err != nil {
		t.Fatal(err)
	}
	if labelWrites(mock) != 2 {
		t.Fatal("refresh replayed creation")
	}
}
func TestExternalLabelDeletionRetiresPendingTargetWithoutModifyingMail(t *testing.T) {
	p, mock, db := labelFixture(t)
	ctx := t.Context()
	if _, err := db.QueueMailMutation(ctx, store.MailMutation{MessageIDs: []string{"a:m1"}, OpType: "move_to_folder", TargetFolderID: "a:label:Label_work"}); err != nil {
		t.Fatal(err)
	}
	ops, payloads, err := db.ClaimPendingOps(ctx, 1, time.Now().Unix())
	if err != nil || len(ops) != 1 {
		t.Fatal(ops, err)
	}
	mock.Lock()
	delete(mock.LabelItems, "Label_work")
	mock.Unlock()
	if err := p.RefreshLabels(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Replay(ctx, ops[0].OpType, payloads[ops[0].ID]); err == nil {
		t.Fatal("replayed absent target")
	}
	active, _ := db.PendingMailOps(ctx, "a", 10)
	if len(active) != 1 || active[0].Attempts != store.MaxPendingMailOpAttempts || active[0].Status != "failed" {
		t.Fatal(active)
	}
	if claimed, _, err := db.ClaimPendingOps(ctx, 10, time.Now().Add(time.Hour).Unix()); err != nil || len(claimed) != 0 {
		t.Fatal(claimed, err)
	}
	var payload store.MailOpPayload
	if err := json.Unmarshal(payloads[ops[0].ID], &payload); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Message(ctx, "a:m1"); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := p.RefreshLabels(cancelled); err == nil {
		t.Fatal("cancel ignored")
	}
}
