package app

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/duanyongcheng/gomail/internal/store"
	"github.com/duanyongcheng/gomail/internal/testgmail"
)

func retainedCloudOutboxFixture(t *testing.T) (*ComposeService, *testgmail.Mock, SendRequest, SaveDraftRequest) {
	t.Helper()
	compose, mock, request := cloudOutboxFixture(t)
	path, err := compose.Stage(t.Context(), StageComposeRequest{Filename: "latest-private.txt", Bytes: []int{0, 1, 2, 255}})
	if err != nil {
		t.Fatal(err)
	}
	saved := draftFixture()
	saved.ExistingDraftID, saved.ClientKey = request.LocalDraftID, "bound-draft"
	saved.Subject, saved.BodyText = "Latest unsent subject", "UNSENT LATEST LOCAL BODY"
	saved.BodyHTML, saved.RawSource = "<p><strong>Latest body</strong></p>", "**Latest body**"
	saved.RenderedSource = &saved.RawSource
	saved.To, saved.AttachmentPaths = request.To, []string{path}
	if _, err := compose.Save(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	request.Subject, request.BodyText, request.BodyHTML = saved.Subject, saved.BodyText, saved.BodyHTML+saved.QuotedReplyHTML
	request.CC, request.BCC, request.InReplyTo = saved.CC, saved.BCC, saved.InReplyTo
	request.AttachmentPaths = saved.AttachmentPaths
	return compose, mock, request, saved
}

func queueAndCleanCloudDraft(t *testing.T, compose *ComposeService, request SendRequest) store.OutboxRecord {
	t.Helper()
	ctx := t.Context()
	before, err := compose.Runtime.DB.ComposeDraft(ctx, request.AccountID, request.LocalDraftID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&OutboxService{Runtime: compose.Runtime}).Queue(ctx, request); err != nil {
		t.Fatal(err)
	}
	// These are the actual ComposeView post-queue cleanup service calls. Neither
	// may delete the recoverable snapshot or the last reference to its staging.
	if _, err := compose.Delete(ctx, DraftRequest{AccountID: request.AccountID, DraftID: request.LocalDraftID}); err != nil {
		t.Fatal(err)
	}
	for _, path := range request.AttachmentPaths {
		if _, err := compose.Cleanup(ctx, CleanupComposeRequest{Path: path}); err != nil {
			t.Fatal(err)
		}
	}
	after, err := compose.Runtime.DB.ComposeDraft(ctx, request.AccountID, request.LocalDraftID)
	for _, private := range []string{request.Subject, request.BodyText, "Latest body", "original quote", "latest-private.txt"} {
		if bytes.Contains(after.Data, []byte(private)) {
			t.Fatal("reserved draft was not encrypted")
		}
	}
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("UI cleanup altered reserved local bytes/revision", err)
	}
	rows, err := compose.Runtime.DB.OutboxList(ctx, request.AccountID)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	row, err := compose.Runtime.DB.Outbox(ctx, request.AccountID, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func assertCloudDraftProtected(t *testing.T, compose *ComposeService, request SendRequest, saved SaveDraftRequest) {
	t.Helper()
	ctx, r := t.Context(), compose.Runtime
	if _, err := compose.Get(ctx, DraftRequest{AccountID: "a", DraftID: request.LocalDraftID}); !errors.Is(err, store.ErrDraftSendProtected) {
		t.Fatal("Gmail browser Get path exposed an editable reserved draft", err)
	}
	for _, existingID := range []string{request.LocalDraftID, ""} {
		save := saved
		save.ExistingDraftID = existingID
		if _, err := compose.Save(ctx, save); !errors.Is(err, store.ErrDraftSendProtected) {
			t.Fatal("reserved draft resaved by ID/client key", err)
		}
	}
	second := request
	second.ClientKey = "different-send-key"
	if _, err := (&OutboxService{Runtime: r}).Queue(ctx, second); err == nil {
		t.Fatal("reserved draft queued a second send")
	}
	copy, err := r.DB.GmailDraftCopy(ctx, "a", request.LocalDraftID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compose.CloudImport(ctx, CloudDraftImportRequest{AccountID: "a", RemoteDraftID: copy.RemoteID}); err == nil {
		t.Fatal("import bypassed send reservation")
	}
	for _, action := range []string{"upload", "delete", "keep-local", "import-remote"} {
		if _, err := compose.Cloud(ctx, CloudDraftRequest{AccountID: "a", DraftID: request.LocalDraftID, Action: action}); err == nil {
			t.Fatal("cloud action bypassed send reservation", action)
		}
	}
}

func TestCloudOutboxCancelRetainsLatestSnapshotAcrossCleanupAndRestart(t *testing.T) {
	for _, preflight := range []string{"queued", "changed", "missing"} {
		t.Run(preflight, func(t *testing.T) {
			compose, mock, request, saved := retainedCloudOutboxFixture(t)
			ctx, r := t.Context(), compose.Runtime
			before, err := compose.Get(ctx, DraftRequest{AccountID: "a", DraftID: request.LocalDraftID})
			if err != nil {
				t.Fatal(err)
			}
			row := queueAndCleanCloudDraft(t, compose, request)
			copy, _ := r.DB.GmailDraftCopy(ctx, "a", request.LocalDraftID)
			mock.Lock()
			remote := mock.Drafts[copy.RemoteID]
			if preflight == "changed" {
				remote.Message.ID = "external-edit"
				mock.Drafts[copy.RemoteID] = remote
			} else if preflight == "missing" {
				delete(mock.Drafts, copy.RemoteID)
			}
			mock.Unlock()
			raw, err := base64.RawURLEncoding.DecodeString(remote.Message.Raw)
			if err != nil || bytes.Contains(raw, []byte(saved.BodyText)) {
				t.Fatal("fixture must leave latest local edits absent from Gmail", err)
			}
			if preflight != "queued" {
				if _, err := r.processOutbox(ctx); err != nil {
					t.Fatal(err)
				}
				failed, err := r.DB.Outbox(ctx, "a", row.ID)
				if err != nil || failed.Status != "failed" || failed.Retryable {
					t.Fatal(failed, err)
				}
			}
			r = reopenWriteDurability(t, r, mock)
			compose = &ComposeService{Runtime: r}
			if err := r.DB.RecoverOutbox(ctx); err != nil {
				t.Fatal(err)
			}
			assertCloudDraftProtected(t, compose, request, saved)
			outbox := &OutboxService{Runtime: r}
			if _, err := outbox.Action(ctx, OutboxActionRequest{AccountID: "a", ID: row.ID, Action: "cancel"}); err != nil {
				t.Fatal(err)
			}
			got, err := compose.Get(ctx, DraftRequest{AccountID: "a", DraftID: request.LocalDraftID})
			if err != nil || !reflect.DeepEqual(before, got) {
				t.Fatal("cancel lost latest body/source/quote/attachment snapshot", got, err)
			}
			cipher, err := r.composeCrypto(ctx)
			if err != nil {
				t.Fatal(err)
			}
			attachment, err := compose.attachmentData(ctx, cipher, saved.AttachmentPaths[0])
			if err != nil || !bytes.Equal(attachment.Bytes, []byte{0, 1, 2, 255}) {
				t.Fatal("cancel lost attachment bytes", err)
			}
			row, _ = r.DB.Outbox(ctx, "a", row.ID)
			copy, _ = r.DB.GmailDraftCopy(ctx, "a", request.LocalDraftID)
			if row.Status != "cancelled" || row.Data != nil || row.Summary != nil || copy.State != "synced" || copy.OutboxID != "" {
				t.Fatal("cancel did not release the reservation atomically", row.Status, copy.State)
			}
			if _, err := outbox.Queue(ctx, request); err == nil {
				t.Fatal("cancel lost send-key tombstone")
			}
			saved.BodyText += " edited after cancellation"
			if _, err := compose.Save(ctx, saved); err != nil {
				t.Fatal("cancel did not restore editability", err)
			}
			if preflight != "queued" {
				if _, err := compose.Cloud(ctx, CloudDraftRequest{AccountID: "a", DraftID: request.LocalDraftID, Action: "check"}); err == nil {
					t.Fatal("external conflict unexpectedly verified")
				}
				copy, _ = r.DB.GmailDraftCopy(ctx, "a", request.LocalDraftID)
				if copy.State != "conflict" {
					t.Fatal("cancelled draft cannot enter conflict recovery", copy.State)
				}
			}
			mock.Lock()
			sends := len(mock.SentRaw)
			mock.Unlock()
			if sends != 0 {
				t.Fatal("unsubmitted draft was sent", sends)
			}
		})
	}
}

func TestCloudOutboxUnknownProtectsSnapshotUntilConfirmedThenCannotRecoverOrResend(t *testing.T) {
	compose, mock, request, saved := retainedCloudOutboxFixture(t)
	ctx, r := t.Context(), compose.Runtime
	row := queueAndCleanCloudDraft(t, compose, request)
	mock.Lock()
	mock.LoseSendResponse = true
	mock.Unlock()
	if _, err := r.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	r = reopenWriteDurability(t, r, mock)
	compose = &ComposeService{Runtime: r}
	if err := r.DB.RecoverOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	assertCloudDraftProtected(t, compose, request, saved)
	outbox := &OutboxService{Runtime: r}
	for _, action := range []string{"retry", "cancel", "dismiss"} {
		if _, err := outbox.Action(ctx, OutboxActionRequest{AccountID: "a", ID: row.ID, Action: action}); err == nil {
			t.Fatal("unknown reservation released", action)
		}
	}
	unknown, err := r.DB.Outbox(ctx, "a", row.ID)
	if err != nil || unknown.Status != "outcome_unknown" || !bytes.Equal(row.Data, unknown.Data) {
		t.Fatal("unknown lost evidence", err)
	}
	injectPendingSQL(t, r.DB, `CREATE TRIGGER fail_check_cleanup BEFORE DELETE ON compose_draft_attachments BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := outbox.Action(ctx, OutboxActionRequest{AccountID: "a", ID: row.ID, Action: "check"}); err == nil {
		t.Fatal("partial Check Sent receipt accepted")
	}
	stillUnknown, _ := r.DB.Outbox(ctx, "a", row.ID)
	if !reflect.DeepEqual(unknown, stillUnknown) {
		t.Fatal("failed Check Sent did not roll back receipt")
	}
	assertCloudDraftProtected(t, compose, request, saved)
	injectPendingSQL(t, r.DB, "DROP TRIGGER fail_check_cleanup")
	if _, err := outbox.Action(ctx, OutboxActionRequest{AccountID: "a", ID: row.ID, Action: "check"}); err != nil {
		t.Fatal(err)
	}
	stored, err := r.DB.ComposeDraft(ctx, "a", request.LocalDraftID)
	if err != nil || !stored.Deleted || stored.Data != nil {
		t.Fatal("confirmed send retained editable bytes", err)
	}
	attachmentID, _ := composeID(saved.AttachmentPaths[0])
	if _, err := r.DB.ComposeAttachment(ctx, attachmentID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("confirmed send retained released staging", err)
	}
	for _, action := range []string{"retry", "cancel"} {
		if _, err := outbox.Action(ctx, OutboxActionRequest{AccountID: "a", ID: row.ID, Action: action}); err == nil {
			t.Fatal("confirmed send recovered for resending", action)
		}
	}
	if _, err := outbox.Action(ctx, OutboxActionRequest{AccountID: "a", ID: row.ID, Action: "dismiss"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{request.LocalDraftID, ""} {
		saved.ExistingDraftID = id
		if _, err := compose.Save(ctx, saved); !errors.Is(err, store.ErrDraftConflict) {
			t.Fatal("sent draft resurrected", err)
		}
	}
	for _, key := range []string{request.ClientKey, "different-send-key"} {
		request.ClientKey = key
		if _, err := outbox.Queue(ctx, request); err == nil {
			t.Fatal("dismissed send resurrected")
		}
	}
	copy, _ := r.DB.GmailDraftCopy(ctx, "a", request.LocalDraftID)
	if copy.State != "sent" || copy.OutboxID != row.ID {
		t.Fatal("dismiss removed sent association", copy)
	}
	if _, err := compose.CloudImport(ctx, CloudDraftImportRequest{AccountID: "a", RemoteDraftID: copy.RemoteID}); err == nil {
		t.Fatal("sent association imported as fresh draft")
	}
	for range 2 {
		if _, err := r.processOutbox(ctx); err != nil {
			t.Fatal(err)
		}
	}
	mock.Lock()
	sends := mock.DraftSends
	mock.Unlock()
	if sends != 1 {
		t.Fatal("unknown/confirmation/cleanup sent more than once", sends)
	}
}

func TestCloudOutboxRetentionTransactionFailureRollsBack(t *testing.T) {
	for _, operation := range []string{"queue", "cancel", "receipt"} {
		for _, kind := range []string{"statement", "commit"} {
			t.Run(operation+"/"+kind, func(t *testing.T) {
				compose, mock, request, saved := retainedCloudOutboxFixture(t)
				ctx, r := t.Context(), compose.Runtime
				beforeDraft, err := r.DB.ComposeDraft(ctx, "a", request.LocalDraftID)
				if err != nil {
					t.Fatal(err)
				}
				var row store.OutboxRecord
				if operation != "queue" {
					row = queueAndCleanCloudDraft(t, compose, request)
				}
				beforeCopy, err := r.DB.GmailDraftCopy(ctx, "a", request.LocalDraftID)
				if err != nil {
					t.Fatal(err)
				}
				attachmentID, _ := composeID(saved.AttachmentPaths[0])
				beforeAttachment, err := r.DB.ComposeAttachment(ctx, attachmentID)
				if err != nil {
					t.Fatal(err)
				}
				event, condition := "UPDATE OF state ON gmail_draft_copies", "WHEN NEW.state='sending'"
				if operation == "cancel" {
					condition = "WHEN NEW.state='synced'"
				} else if operation == "receipt" {
					// Fail after the receipt, sent association and draft tombstone
					// have all been written, at the final attachment cleanup step.
					event, condition = "DELETE ON compose_attachments", ""
				}
				injectWriteDurabilityFault(t, r.DB, event, condition, kind)
				outbox := &OutboxService{Runtime: r}
				if operation == "queue" {
					_, err = outbox.Queue(ctx, request)
				} else if operation == "cancel" {
					_, err = outbox.Action(ctx, OutboxActionRequest{AccountID: "a", ID: row.ID, Action: "cancel"})
				} else {
					_, err = r.processOutbox(ctx)
				}
				if err == nil {
					t.Fatal("transaction failure was hidden")
				}
				afterDraft, err := r.DB.ComposeDraft(ctx, "a", request.LocalDraftID)
				if err != nil || !reflect.DeepEqual(beforeDraft, afterDraft) {
					t.Fatal("failed transaction changed recoverable snapshot", err)
				}
				afterCopy, err := r.DB.GmailDraftCopy(ctx, "a", request.LocalDraftID)
				if err != nil || !reflect.DeepEqual(beforeCopy, afterCopy) {
					t.Fatal("failed transaction changed reservation", err)
				}
				afterAttachment, err := r.DB.ComposeAttachment(ctx, attachmentID)
				if err != nil || !reflect.DeepEqual(beforeAttachment, afterAttachment) {
					t.Fatal("failed transaction changed attachment bytes", err)
				}
				// Prove the reference rollback as well as the attachment row: an
				// explicit release must still be unable to remove saved bytes.
				if _, err := compose.Cleanup(ctx, CleanupComposeRequest{Path: saved.AttachmentPaths[0]}); err != nil {
					t.Fatal("failed transaction lost attachment reference", err)
				}
				if operation == "queue" {
					rows, err := r.DB.OutboxList(ctx, "a")
					if err != nil || len(rows) != 0 {
						t.Fatal("failed reservation retained an outbox row", err)
					}
				} else {
					after, err := r.DB.Outbox(ctx, "a", row.ID)
					if err != nil || !bytes.Equal(row.Data, after.Data) || !bytes.Equal(row.Summary, after.Summary) {
						t.Fatal("failed transaction cleared immutable outbox", err)
					}
					if operation == "cancel" && !reflect.DeepEqual(row, after) {
						t.Fatal("failed cancellation changed outbox state")
					}
					if operation == "receipt" {
						if after.Status != "in_progress" || r.outboxSettlement == nil {
							t.Fatal("receipt failure lost durable barrier or memory receipt")
						}
						for range 2 {
							if _, err := r.processOutbox(ctx); err == nil {
								t.Fatal("receipt fault unexpectedly repaired")
							}
						}
					}
				}
				mock.Lock()
				sends := mock.DraftSends
				mock.Unlock()
				wantSends := 0
				if operation == "receipt" {
					wantSends = 1
				}
				if sends != wantSends {
					t.Fatal("transaction failure repeated or permitted HTTP send", sends)
				}
				injectPendingSQL(t, r.DB, "DROP TRIGGER durability_write_fault")
				if operation == "queue" {
					row = queueAndCleanCloudDraft(t, compose, request)
				}
				if operation != "receipt" {
					if _, err := outbox.Action(ctx, OutboxActionRequest{AccountID: "a", ID: row.ID, Action: "cancel"}); err != nil {
						t.Fatal(err)
					}
					if _, err := compose.Get(ctx, DraftRequest{AccountID: "a", DraftID: request.LocalDraftID}); err != nil {
						t.Fatal("repaired cancellation did not recover draft", err)
					}
				} else {
					if _, err := r.processOutbox(ctx); err != nil {
						t.Fatal(err)
					}
					if _, err := r.DB.ComposeAttachment(ctx, attachmentID); !errors.Is(err, sql.ErrNoRows) {
						t.Fatal("receipt repair did not release attachment", err)
					}
					stored, _ := r.DB.ComposeDraft(ctx, "a", request.LocalDraftID)
					row, _ = r.DB.Outbox(ctx, "a", row.ID)
					if !stored.Deleted || stored.Data != nil || row.Status != "sent" || row.RemoteID == "" {
						t.Fatal("receipt repair did not commit cleanup and acceptance")
					}
				}
			})
		}
	}
}

func TestOutboxLocalOnlyQueueCleanupStillDeletesDraft(t *testing.T) {
	compose, _ := composeFixture(t)
	ctx, r := t.Context(), compose.Runtime
	path, err := compose.Stage(ctx, StageComposeRequest{Filename: "local.txt", Bytes: []int{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	saved := draftFixture()
	saved.AttachmentPaths = []string{path}
	id, err := compose.Save(ctx, saved)
	if err != nil {
		t.Fatal(err)
	}
	request := sendRequest()
	request.LocalDraftID, request.AttachmentPaths = id, saved.AttachmentPaths
	outbox := &OutboxService{Runtime: r}
	if _, err := outbox.Queue(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, err := compose.Delete(ctx, DraftRequest{AccountID: "a", DraftID: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := compose.Cleanup(ctx, CleanupComposeRequest{Path: path}); err != nil {
		t.Fatal(err)
	}
	stored, err := r.DB.ComposeDraft(ctx, "a", id)
	if err != nil || !stored.Deleted || stored.Data != nil {
		t.Fatal("unlinked cleanup no longer deletes draft", err)
	}
	attachmentID, _ := composeID(path)
	if _, err := r.DB.ComposeAttachment(ctx, attachmentID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("unlinked cleanup leaked staging", err)
	}
	items, err := r.DB.OutboxList(ctx, "a")
	if err != nil || len(items) != 1 || items[0].HasCloudDraft {
		t.Fatal(items, err)
	}
	if _, err := outbox.Action(ctx, OutboxActionRequest{AccountID: "a", ID: items[0].ID, Action: "cancel"}); err != nil {
		t.Fatal("unlinked cancellation newly blocked", err)
	}
}

func TestCloudOutboxLegacyDeletedSnapshotCancelFailsClosed(t *testing.T) {
	for _, status := range []string{"queued", "failed"} {
		t.Run(status, func(t *testing.T) {
			compose, mock, request, _ := retainedCloudOutboxFixture(t)
			ctx, r := t.Context(), compose.Runtime
			row := queueAndCleanCloudDraft(t, compose, request)
			// Reproduce the old release's intentional post-queue deletion, not the
			// now-protected public Delete. This is compatibility data on disk.
			injectPendingSQL(t, r.DB, `UPDATE compose_drafts SET data=NULL,deleted=1;
				DELETE FROM compose_draft_attachments; DELETE FROM compose_attachments;`)
			if status == "failed" {
				if _, err := r.DB.ClaimOutbox(ctx, row.CreatedAt+1); err != nil {
					t.Fatal(err)
				}
				if err := r.DB.SettleOutbox(ctx, row.ID, "failed", "preflight conflict", "", false); err != nil {
					t.Fatal(err)
				}
			}
			r = reopenWriteDurability(t, r, mock)
			before, _ := r.DB.Outbox(ctx, "a", row.ID)
			if _, err := (&OutboxService{Runtime: r}).Action(ctx, OutboxActionRequest{AccountID: "a", ID: row.ID, Action: "cancel"}); !errors.Is(err, store.ErrOutboxDraftUnavailable) || !strings.Contains(err.Error(), "preserved") {
				t.Fatal("legacy cancellation did not explain retained content", err)
			}
			after, _ := r.DB.Outbox(ctx, "a", row.ID)
			if !reflect.DeepEqual(before, after) || !bytes.Equal(row.Data, after.Data) || !bytes.Equal(row.Summary, after.Summary) {
				t.Fatal("legacy cancellation destroyed the last encrypted message")
			}
			copy, _ := r.DB.GmailDraftCopy(ctx, "a", request.LocalDraftID)
			if copy.State != "sending" || copy.OutboxID != row.ID {
				t.Fatal("legacy cancellation released reservation")
			}
		})
	}
}
