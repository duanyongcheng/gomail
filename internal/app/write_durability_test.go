package app

import (
	"bytes"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/provider/gmail"
	"github.com/duanyongcheng/pebble-go/internal/store"
	"github.com/duanyongcheng/pebble-go/internal/testgmail"
)

func writeDurabilityFixture(t *testing.T) (*ComposeService, *testgmail.Mock) {
	t.Helper()
	compose, _ := composeFixture(t)
	mock := testgmail.NewMock()
	t.Cleanup(mock.Close)
	mock.ProfileEmail = "a@example.invalid"
	compose.Runtime.InstallProvider("a", gmail.New(compose.Runtime.DB, mock.API(staticToken{"demo-access"}), "a"))
	return compose, mock
}

func reopenWriteDurability(t *testing.T, r *Runtime, mock *testgmail.Mock) *Runtime {
	t.Helper()
	r.Scheduler.Close()
	if err := r.DB.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), r.DB.Path())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	next := New(t.Context(), db, r.Keyring, nil)
	t.Cleanup(next.Scheduler.Close)
	next.InstallProvider("a", gmail.New(db, mock.API(staticToken{"demo-access"}), "a"))
	return next
}

// Exercise both a rejected statement and a failure at commit, after the actual
// intent statement succeeds. The latter uses a deferred FK in a test-only table;
// modernc rolls the failed commit back before returning the connection to sql.DB.
// The autocommit BeginOutboxSend UPDATE checks deferred FKs at statement end.
func injectWriteDurabilityFault(t *testing.T, db *store.DB, event, condition, kind string) {
	t.Helper()
	timing, body := "BEFORE", "SELECT RAISE(ABORT,'injected durability failure');"
	if kind == "commit" {
		injectPendingSQL(t, db, `CREATE TABLE durability_commit_fault(account_id TEXT REFERENCES accounts(id) DEFERRABLE INITIALLY DEFERRED)`)
		timing, body = "AFTER", "INSERT INTO durability_commit_fault VALUES('durability-missing-account');"
	}
	injectPendingSQL(t, db, "CREATE TRIGGER durability_write_fault "+timing+" "+event+" "+condition+" BEGIN "+body+" END")
}

func assertNoDurabilityWrites(t *testing.T, mock *testgmail.Mock, since int) {
	t.Helper()
	mock.Lock()
	defer mock.Unlock()
	for _, request := range mock.Requests[since:] {
		if !strings.HasPrefix(request, "GET ") {
			t.Fatalf("intent persistence failed but remote write was attempted: %s", request)
		}
	}
}

func TestOutboxQueueDurabilityFailurePreservesDraftAndNeverSends(t *testing.T) {
	for _, kind := range []string{"statement", "commit"} {
		t.Run(kind, func(t *testing.T) {
			compose, mock := writeDurabilityFixture(t)
			r, ctx := compose.Runtime, t.Context()
			attachment, err := compose.Stage(ctx, StageComposeRequest{Filename: "fixture.txt", Bytes: []int{1, 2, 3}})
			if err != nil {
				t.Fatal(err)
			}
			draftID, err := compose.Save(ctx, SaveDraftRequest{AccountID: "a", Subject: "saved fixture", BodyText: "keep local content", EditorMode: "rich", AttachmentPaths: []string{attachment}})
			if err != nil {
				t.Fatal(err)
			}
			before, err := r.DB.ComposeDraft(ctx, "a", draftID)
			if err != nil {
				t.Fatal(err)
			}
			events := 0
			r.OnOutboxChanged = func() { events++ }
			injectWriteDurabilityFault(t, r.DB, "INSERT ON outbox", "", kind)
			request := sendRequest()
			request.AttachmentPaths = []string{attachment}
			if _, err := (&OutboxService{Runtime: r}).Queue(ctx, request); err == nil {
				t.Fatal("queue persistence failure was hidden")
			}
			if events != 0 {
				t.Fatal("failed queue announced success")
			}
			if _, err := r.processOutbox(ctx); err != nil {
				t.Fatal(err)
			}
			r = reopenWriteDurability(t, r, mock)
			if _, err := r.processOutbox(ctx); err != nil {
				t.Fatal(err)
			}
			rows, err := r.DB.OutboxList(ctx, "a")
			if err != nil || len(rows) != 0 {
				t.Fatalf("failed queue persisted: %+v, %v", rows, err)
			}
			after, err := r.DB.ComposeDraft(ctx, "a", draftID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("failed queue altered local draft", err)
			}
			compose = &ComposeService{Runtime: r}
			draft, err := compose.Get(ctx, DraftRequest{AccountID: "a", DraftID: draftID})
			if err != nil || len(draft.Attachments) != 1 || draft.Attachments[0].Path != attachment {
				t.Fatal("failed queue lost saved attachment", err)
			}
			cipher, err := r.composeCrypto(ctx)
			if err != nil {
				t.Fatal(err)
			}
			data, err := compose.attachmentData(ctx, cipher, attachment)
			if err != nil || !bytes.Equal(data.Bytes, []byte{1, 2, 3}) {
				t.Fatal("failed queue lost saved attachment bytes", err)
			}
			mock.Lock()
			calls := len(mock.Requests)
			mock.Unlock()
			if calls != 0 {
				t.Fatalf("failed queue caused %d network requests", calls)
			}
		})
	}
}

func TestOutboxIntentDurabilityFailureNeverSubmitsAcrossReopen(t *testing.T) {
	for _, stage := range []string{"claim", "begin"} {
		for _, kind := range []string{"statement", "commit"} {
			t.Run(stage+"/"+kind, func(t *testing.T) {
				compose, mock := writeDurabilityFixture(t)
				r, ctx := compose.Runtime, t.Context()
				if _, err := (&OutboxService{Runtime: r}).Queue(ctx, sendRequest()); err != nil {
					t.Fatal(err)
				}
				rows, err := r.DB.OutboxList(ctx, "a")
				if err != nil || len(rows) != 1 {
					t.Fatal(rows, err)
				}
				before, err := r.DB.Outbox(ctx, "a", rows[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				transition, wantStatus, wantAttempts := "preparing", "queued", 0
				if stage == "begin" {
					transition, wantStatus, wantAttempts = "in_progress", "failed", 1
				}
				injectWriteDurabilityFault(t, r.DB, "UPDATE OF status ON outbox", "WHEN NEW.status='"+transition+"'", kind)
				_, err = r.processOutbox(ctx)
				if (stage == "claim") != (err != nil) {
					t.Fatalf("process error = %v for %s failure", err, stage)
				}
				assertNoDurabilityWrites(t, mock, 0)
				r = reopenWriteDurability(t, r, mock)
				after, err := r.DB.Outbox(ctx, "a", before.ID)
				if err != nil || after.Status != wantStatus || after.Attempts != wantAttempts || !bytes.Equal(before.Data, after.Data) || !bytes.Equal(before.Summary, after.Summary) {
					t.Fatalf("failed intent lost state: %+v, %v", after, err)
				}
				// Explicit retry still must not bypass a failed pre-send barrier.
				if stage == "begin" {
					if err := r.DB.OutboxAction(ctx, "a", before.ID, "retry"); err != nil {
						t.Fatal(err)
					}
				}
				_, err = r.processOutbox(ctx)
				if (stage == "claim") != (err != nil) {
					t.Fatalf("reopened process error = %v", err)
				}
				assertNoDurabilityWrites(t, mock, 0)
				injectPendingSQL(t, r.DB, "DROP TRIGGER durability_write_fault")
				if stage == "begin" {
					if err := r.DB.OutboxAction(ctx, "a", before.ID, "retry"); err != nil {
						t.Fatal(err)
					}
				}
				for range 2 {
					if _, err := r.processOutbox(ctx); err != nil {
						t.Fatal(err)
					}
				}
				mock.Lock()
				sends := len(mock.SentRaw)
				mock.Unlock()
				if sends != 1 {
					t.Fatalf("repaired store must send exactly once, got %d", sends)
				}
			})
		}
	}
}

func TestCloudIntentDurabilityFailureNeverCreatesOrUpdates(t *testing.T) {
	for _, action := range []string{"create", "update"} {
		for _, kind := range []string{"statement", "commit"} {
			t.Run(action+"/"+kind, func(t *testing.T) {
				compose, mock := writeDurabilityFixture(t)
				r, ctx := compose.Runtime, t.Context()
				draft := SaveDraftRequest{AccountID: "a", Subject: "cloud fixture", BodyText: "first saved content", EditorMode: "rich"}
				id, err := compose.Save(ctx, draft)
				if err != nil {
					t.Fatal(err)
				}
				request := CloudDraftRequest{AccountID: "a", DraftID: id, Action: "upload"}
				var previous store.GmailDraftCopy
				event, condition := "INSERT ON gmail_draft_copies", ""
				if action == "update" {
					if _, err := compose.Cloud(ctx, request); err != nil {
						t.Fatal(err)
					}
					previous, err = r.DB.GmailDraftCopy(ctx, "a", id)
					if err != nil {
						t.Fatal(err)
					}
					draft.ExistingDraftID, draft.BodyText = id, "newer local content"
					if _, err := compose.Save(ctx, draft); err != nil {
						t.Fatal(err)
					}
					event, condition = "UPDATE ON gmail_draft_copies", "WHEN NEW.state='pending'"
				}
				local, err := r.DB.ComposeDraft(ctx, "a", id)
				if err != nil {
					t.Fatal(err)
				}
				mock.Lock()
				calls, writes := len(mock.Requests), mock.DraftWrites
				remoteBefore := mock.Drafts[previous.RemoteID]
				mock.Unlock()
				injectWriteDurabilityFault(t, r.DB, event, condition, kind)
				for attempt := range 2 {
					if _, err := compose.Cloud(ctx, request); err == nil || !strings.Contains(err.Error(), "cannot record cloud draft intent") {
						t.Fatalf("intent failure did not surface: %v", err)
					}
					assertNoDurabilityWrites(t, mock, calls)
					copy, err := r.DB.GmailDraftCopy(ctx, "a", id)
					if action == "create" {
						if !errors.Is(err, sql.ErrNoRows) {
							t.Fatalf("failed create left intent: %+v, %v", copy, err)
						}
					} else if err != nil || !reflect.DeepEqual(copy, previous) {
						t.Fatal("failed update replaced earlier cloud receipt", err)
					}
					if attempt == 0 {
						r = reopenWriteDurability(t, r, mock)
						compose = &ComposeService{Runtime: r}
					}
				}
				localAfter, err := r.DB.ComposeDraft(ctx, "a", id)
				if err != nil || !reflect.DeepEqual(localAfter, local) {
					t.Fatal("failed cloud intent altered local snapshot", err)
				}
				mock.Lock()
				remoteAfter, writesAfter := mock.Drafts[previous.RemoteID], mock.DraftWrites
				mock.Unlock()
				if !reflect.DeepEqual(remoteBefore, remoteAfter) || writesAfter != writes {
					t.Fatal("failed intent changed remote content")
				}
				injectPendingSQL(t, r.DB, "DROP TRIGGER durability_write_fault")
				if status, err := compose.Cloud(ctx, request); err != nil || status.State != "synced" {
					t.Fatal("explicit upload after repair failed", status, err)
				}
				request.Action = "check"
				if _, err := compose.Cloud(ctx, request); err != nil {
					t.Fatal(err)
				}
				mock.Lock()
				writesAfter, copies := mock.DraftWrites, len(mock.Drafts)
				mock.Unlock()
				if writesAfter != writes+1 || copies != 1 {
					t.Fatalf("repair/check duplicated write: writes=%d, copies=%d", writesAfter, copies)
				}
			})
		}
	}
}
