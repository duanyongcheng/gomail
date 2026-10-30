package store

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestCloudOutboxStoreSaveGuardAndCleanupProtectLatestSnapshot(t *testing.T) {
	for _, status := range []string{"queued", "failed", "outcome_unknown", "sent"} {
		t.Run(status, func(t *testing.T) {
			db, record := cloudOutboxStoreFixture(t)
			ctx := t.Context()
			draft, err := db.ComposeDraft(ctx, "a", "local")
			if err != nil {
				t.Fatal(err)
			}
			if err := db.StageComposeAttachment(ctx, EncryptedComposeAttachment{ID: "attachment", Data: []byte("encrypted-attachment"), Size: 3}); err != nil {
				t.Fatal(err)
			}
			draft.Data = []byte("encrypted-latest-local")
			// The send path's preceding save is allowed before reservation.
			if err := db.SaveComposeDraft(ctx, draft, []string{"attachment"}); err != nil {
				t.Fatal(err)
			}
			draft, _ = db.ComposeDraft(ctx, "a", "local")
			record.CloudDraft.LocalRevision = draft.Revision
			if _, err := db.QueueOutbox(ctx, record); err != nil {
				t.Fatal(err)
			}
			if status != "queued" {
				if _, err := db.ClaimOutbox(ctx, time.Now().Unix()); err != nil {
					t.Fatal(err)
				}
				if err := db.SettleOutbox(ctx, record.ID, status, "", "", false); err != nil {
					t.Fatal(err)
				}
			}
			if status == "sent" {
				// Older releases may have a still-live draft with a sent
				// association. The guard must work independently of tombstones.
				execute(t, db.write, "UPDATE compose_drafts SET deleted=0,data=X'010203' WHERE id='local'")
			}
			before, _ := db.ComposeDraft(ctx, "a", "local")
			replacement := before
			replacement.Data = []byte("unwanted-replacement")
			if err := db.SaveComposeDraft(ctx, replacement, nil); !errors.Is(err, ErrDraftSendProtected) {
				t.Fatal("store accepted a reserved/sent save", err)
			}
			after, _ := db.ComposeDraft(ctx, "a", "local")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("blocked save changed stored bytes/revision")
			}
			if status == "sent" {
				return
			}
			if err := db.DeleteComposeDraft(ctx, "a", "local"); err != nil {
				t.Fatal(err)
			}
			if err := db.ReleaseComposeAttachment(ctx, "attachment"); err != nil {
				t.Fatal(err)
			}
			after, _ = db.ComposeDraft(ctx, "a", "local")
			attachment, err := db.ComposeAttachment(ctx, "attachment")
			if err != nil || !reflect.DeepEqual(before, after) || !bytes.Equal(attachment.Data, []byte("encrypted-attachment")) {
				t.Fatal("cleanup destroyed protected draft/refs", err)
			}
			if status == "outcome_unknown" {
				if err := db.OutboxAction(ctx, "a", record.ID, "cancel"); err == nil {
					t.Fatal("unknown cancel bypassed quarantine")
				}
				return
			}
			if err := db.OutboxAction(ctx, "a", record.ID, "cancel"); err != nil {
				t.Fatal(err)
			}
			if err := db.SaveComposeDraft(ctx, replacement, []string{"attachment"}); err != nil {
				t.Fatal("cancelled draft remains uneditable", err)
			}
		})
	}
}
