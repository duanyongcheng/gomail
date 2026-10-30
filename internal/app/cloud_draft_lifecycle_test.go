package app

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/bary/pebble-go/internal/mailmime"
	"github.com/bary/pebble-go/internal/provider/gmail"
	"github.com/bary/pebble-go/internal/testgmail"
)

func lifecycleFixture(t *testing.T) (*ComposeService, *testgmail.Mock) {
	t.Helper()
	service, _ := composeFixture(t)
	mock := testgmail.NewMock()
	t.Cleanup(mock.Close)
	mock.ProfileEmail = "a@example.invalid"
	service.Runtime.InstallProvider("a", gmail.New(service.Runtime.DB, mock.API(staticToken{"demo-access"}), "a"))
	return service, mock
}

func lifecycleRaw(t *testing.T, subject string) []byte {
	t.Helper()
	raw, err := mailmime.BuildDraft(mailmime.Outgoing{
		From: mail.Address{Name: "Primary", Address: "a@example.invalid"},
		To:   []mail.Address{{Address: "to@example.invalid"}}, CC: []mail.Address{{Address: "cc@example.invalid"}},
		BCC: []mail.Address{{Address: "private@example.invalid"}}, IncludeBcc: true,
		Subject: subject, Text: "remote text", HTML: `<p>remote text<img src="cid:logo@example.invalid"></p>`,
		MessageID: "draft@example.invalid", Date: time.Unix(1700000000, 0),
		InReplyTo: "parent@example.invalid", References: []string{"grandparent@example.invalid", "parent@example.invalid"},
		Attachments: []mailmime.Attachment{
			{Filename: "report.bin", MIMEType: "application/x-pebble", ContentID: "report@example.invalid", Data: []byte{0, 1, 2, 255}},
			{Filename: "logo.png", MIMEType: "image/png", ContentID: "logo@example.invalid", Inline: true, Data: []byte("inline bytes")},
		},
	}, mailmime.MaxOutgoingBytes)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func lifecycleSQL(t *testing.T, service *ComposeService, query string) {
	t.Helper()
	db, err := sql.Open("sqlite", service.Runtime.DB.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func TestCloudLifecycleListImportEncryptedRoundTripAndMetadata(t *testing.T) {
	s, mock := lifecycleFixture(t)
	raw := lifecycleRaw(t, "web draft")
	messageID := mock.SetDraft("web-draft", raw, "native-thread")
	page, err := s.CloudList(t.Context(), CloudDraftListRequest{AccountID: "a"})
	if err != nil || len(page.Items) != 1 || page.Items[0].Subject != "web draft" || page.Items[0].MessageID != messageID || page.Items[0].LocalDraftID != "" {
		t.Fatal(page, err)
	}
	mock.Lock()
	before := append([]string(nil), mock.RequestURIs...)
	mock.Unlock()
	for _, uri := range before {
		if strings.Contains(uri, "format=raw") {
			t.Fatal("list downloaded full MIME")
		}
	}
	d, err := s.CloudImport(t.Context(), CloudDraftImportRequest{AccountID: "a", RemoteDraftID: "web-draft", ExpectedMessageID: messageID})
	if err != nil || d.ID == "" || d.AccountID != "a" || d.Subject != "web draft" || len(d.BCC) != 1 || len(d.Attachments) != 2 || d.RemoteThreadID != "native-thread" || d.References != "grandparent@example.invalid parent@example.invalid" {
		t.Fatal(d, err)
	}
	stored, err := s.Runtime.DB.ComposeDraft(t.Context(), "a", d.ID)
	if err != nil || bytes.Contains(stored.Data, []byte("remote text")) || bytes.Contains(stored.Data, []byte("private@example.invalid")) {
		t.Fatal("plaintext import", err)
	}
	paths := []string{}
	for _, attachment := range d.Attachments {
		paths = append(paths, attachment.Path)
		id, _ := composeID(attachment.Path)
		record, err := s.Runtime.DB.ComposeAttachment(t.Context(), id)
		if err != nil || bytes.Contains(record.Data, []byte(attachment.Name)) || bytes.Contains(record.Data, []byte("inline bytes")) {
			t.Fatal("plaintext attachment", err)
		}
	}
	mock.Lock()
	writes := mock.DraftWrites + mock.DraftDeletes + mock.DraftSends
	mock.Unlock()
	if writes != 0 {
		t.Fatal("import modified Gmail")
	}
	page, err = s.CloudList(t.Context(), CloudDraftListRequest{AccountID: "a"})
	if err != nil || page.Items[0].LocalDraftID != d.ID {
		t.Fatal(page, err)
	}
	again, err := s.CloudImport(t.Context(), CloudDraftImportRequest{AccountID: "a", RemoteDraftID: "web-draft", ExpectedMessageID: messageID})
	if err != nil || again.ID != d.ID {
		t.Fatal("reimport duplicated local draft", again, err)
	}
	if _, err := s.Save(t.Context(), SaveDraftRequest{
		AccountID: "a", ExistingDraftID: d.ID, Subject: "Re: web draft", To: d.To, CC: d.CC, BCC: d.BCC,
		BodyText: d.BodyText, BodyHTML: d.BodyHTML, RawSource: d.RawSource, EditorMode: d.EditorMode,
		InReplyTo: d.InReplyTo, AttachmentPaths: paths,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cloud(t.Context(), CloudDraftRequest{AccountID: "a", DraftID: d.ID, Action: "upload"}); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	updated := mock.Drafts["web-draft"]
	mock.Unlock()
	updatedRaw, _ := base64.RawURLEncoding.DecodeString(updated.Message.Raw)
	parsed, err := mailmime.ParseDraftWithLimit(updatedRaw, mailmime.MaxOutgoingBytes)
	if err != nil || updated.Message.ThreadID != "native-thread" || parsed.Subject != "Re: web draft" || parsed.References != d.References || parsed.InReplyTo != d.InReplyTo ||
		len(parsed.BCC) != 1 || len(parsed.Attachments) != 2 ||
		parsed.Attachments[0].MIMEType != "application/x-pebble" || parsed.Attachments[0].ContentID != "report@example.invalid" ||
		!bytes.Equal(parsed.Attachments[0].Data, []byte{0, 1, 2, 255}) || !parsed.Attachments[1].Inline || parsed.Attachments[1].Filename != "logo.png" || parsed.Attachments[1].ContentID != "logo@example.invalid" {
		t.Fatal(parsed, err)
	}
}

func TestCloudLifecyclePaginationStaleListingAndDraftWithoutDate(t *testing.T) {
	s, mock := lifecycleFixture(t)
	raw := []byte("From: a@example.invalid\r\nSubject: unfinished\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nNot yet addressed")
	for i := 0; i < 23; i++ {
		mock.SetDraft(fmt.Sprintf("draft-%02d", i), raw, "thread")
	}
	page, err := s.CloudList(t.Context(), CloudDraftListRequest{AccountID: "a"})
	if err != nil || len(page.Items) != 20 || page.NextPageToken == "" {
		t.Fatal(page, err)
	}
	last, err := s.CloudList(t.Context(), CloudDraftListRequest{AccountID: "a", PageToken: page.NextPageToken})
	if err != nil || len(last.Items) != 3 || last.NextPageToken != "" {
		t.Fatal(last, err)
	}
	ref := page.Items[0]
	mock.SetDraft(ref.ID, raw, "thread")
	if _, err := s.CloudImport(t.Context(), CloudDraftImportRequest{AccountID: "a", RemoteDraftID: ref.ID, ExpectedMessageID: ref.MessageID}); err == nil {
		t.Fatal("import accepted stale listing")
	}
	d, err := s.CloudImport(t.Context(), CloudDraftImportRequest{AccountID: "a", RemoteDraftID: ref.ID})
	if err != nil || d.Subject != "unfinished" || d.BodyText != "Not yet addressed" || len(d.To) != 0 {
		t.Fatal(d, err)
	}
	if _, err := s.Cloud(t.Context(), CloudDraftRequest{AccountID: "a", DraftID: d.ID, Action: "check"}); err != nil {
		t.Fatal("draft lacking Message-ID/Date cannot reconcile", err)
	}
	if _, err := s.CloudImport(t.Context(), CloudDraftImportRequest{AccountID: "b", RemoteDraftID: ref.ID}); err == nil {
		t.Fatal("cross-account import succeeded")
	}
	mock.SetDraft("alias", bytes.Replace(raw, []byte("a@example.invalid"), []byte("alias@example.invalid"), 1), "thread")
	if _, err := s.CloudImport(t.Context(), CloudDraftImportRequest{AccountID: "a", RemoteDraftID: "alias"}); err == nil {
		t.Fatal("alias silently changed sender identity")
	}
}

func TestCloudLifecycleConflictRecoveryPreservesBothAndDoesNotWriteRemote(t *testing.T) {
	s, mock := lifecycleFixture(t)
	mock.SetDraft("web", lifecycleRaw(t, "original"), "thread")
	d, err := s.CloudImport(t.Context(), CloudDraftImportRequest{AccountID: "a", RemoteDraftID: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(t.Context(), SaveDraftRequest{AccountID: "a", ExistingDraftID: d.ID, Subject: "local edits", BodyText: "keep my local edits"}); err != nil {
		t.Fatal(err)
	}
	mock.SetDraft("web", lifecycleRaw(t, "remote edits"), "thread")
	if _, err := s.CloudImport(t.Context(), CloudDraftImportRequest{AccountID: "a", RemoteDraftID: "web"}); err == nil {
		t.Fatal("implicit import overwrote local edits")
	}
	recovered, err := s.Cloud(t.Context(), CloudDraftRequest{AccountID: "a", DraftID: d.ID, Action: "import-remote"})
	if err != nil || recovered.DraftID == "" || recovered.DraftID == d.ID {
		t.Fatal(recovered, err)
	}
	old, _ := s.Get(t.Context(), DraftRequest{AccountID: "a", DraftID: d.ID})
	fresh, _ := s.Get(t.Context(), DraftRequest{AccountID: "a", DraftID: recovered.DraftID})
	if old.Subject != "local edits" || old.BodyText != "keep my local edits" || fresh.Subject != "remote edits" || len(fresh.Attachments) != 2 {
		t.Fatal(old, fresh)
	}
	if _, err := s.Runtime.DB.GmailDraftCopy(t.Context(), "a", d.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("old draft still bound", err)
	}
	mock.Lock()
	writes := mock.DraftWrites + mock.DraftDeletes + mock.DraftSends
	delete(mock.Drafts, "web")
	mock.Unlock()
	if writes != 0 {
		t.Fatal("conflict recovery wrote Gmail")
	}
	if _, err := s.Cloud(t.Context(), CloudDraftRequest{AccountID: "a", DraftID: fresh.ID, Action: "check"}); err == nil {
		t.Fatal("missing remote not detected")
	}
	result, err := s.Cloud(t.Context(), CloudDraftRequest{AccountID: "a", DraftID: fresh.ID, Action: "keep-local"})
	if err != nil || result.State != "local" {
		t.Fatal(result, err)
	}
	if got, err := s.Get(t.Context(), DraftRequest{AccountID: "a", DraftID: fresh.ID}); err != nil || got.Subject != "remote edits" {
		t.Fatal("detach lost content", got, err)
	}
}

func TestCloudLifecycleDeleteUnknownRestartAndReceiptProtection(t *testing.T) {
	for _, receiptFault := range []bool{false, true} {
		t.Run(fmt.Sprint("receiptFault=", receiptFault), func(t *testing.T) {
			s, mock := lifecycleFixture(t)
			mock.SetDraft("web", lifecycleRaw(t, "keep locally"), "thread")
			d, err := s.CloudImport(t.Context(), CloudDraftImportRequest{AccountID: "a", RemoteDraftID: "web"})
			if err != nil {
				t.Fatal(err)
			}
			if receiptFault {
				lifecycleSQL(t, s, `CREATE TRIGGER fail_cloud_receipt BEFORE DELETE ON gmail_draft_copies BEGIN SELECT RAISE(ABORT,'injected'); END`)
			} else {
				mock.Lock()
				mock.LoseDraftDeleteResponse = true
				mock.Unlock()
			}
			req := CloudDraftRequest{AccountID: "a", DraftID: d.ID, Action: "delete"}
			if _, err := s.Cloud(t.Context(), req); err == nil {
				t.Fatal("uncertain receipt succeeded")
			}
			if _, err := s.Cloud(t.Context(), req); err == nil {
				t.Fatal("retried uncertain delete")
			}
			if _, err := s.Delete(t.Context(), DraftRequest{AccountID: "a", DraftID: d.ID}); err == nil {
				t.Fatal("deleted unresolved local identity")
			}
			if _, err := s.Cloud(t.Context(), CloudDraftRequest{AccountID: "a", DraftID: d.ID, Action: "keep-local"}); err == nil {
				t.Fatal("detached unknown operation")
			}
			if receiptFault {
				lifecycleSQL(t, s, `DROP TRIGGER fail_cloud_receipt`)
			}
			restarted := New(t.Context(), s.Runtime.DB, s.Runtime.Keyring, nil)
			defer restarted.Scheduler.Close()
			p, _ := s.Runtime.provider("a")
			restarted.InstallProvider("a", p)
			s = &ComposeService{Runtime: restarted}
			state, err := s.Cloud(t.Context(), CloudDraftRequest{AccountID: "a", DraftID: d.ID, Action: "check"})
			if err != nil || state.State != "local" {
				t.Fatal(state, err)
			}
			kept, err := s.Get(t.Context(), DraftRequest{AccountID: "a", DraftID: d.ID})
			if err != nil || kept.Subject != "keep locally" || len(kept.Attachments) != 2 {
				t.Fatal(kept, err)
			}
			mock.Lock()
			deletes, remaining := mock.DraftDeletes, len(mock.Drafts)
			mock.Unlock()
			if deletes != 1 || remaining != 0 {
				t.Fatal("delete replayed", deletes, remaining)
			}
		})
	}
}

func TestCloudLifecycleDeleteIntentFailureAndObservedExternalEdit(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint("changed=", changed), func(t *testing.T) {
			s, mock := lifecycleFixture(t)
			mock.SetDraft("web", lifecycleRaw(t, "preserved"), "thread")
			d, err := s.CloudImport(t.Context(), CloudDraftImportRequest{AccountID: "a", RemoteDraftID: "web"})
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				mock.SetDraft("web", lifecycleRaw(t, "later web edit"), "thread")
			} else {
				lifecycleSQL(t, s, `CREATE TRIGGER fail_cloud_delete BEFORE UPDATE OF state ON gmail_draft_copies WHEN NEW.state='delete_pending' BEGIN SELECT RAISE(ABORT,'injected'); END`)
			}
			if _, err := s.Cloud(t.Context(), CloudDraftRequest{AccountID: "a", DraftID: d.ID, Action: "delete"}); err == nil {
				t.Fatal("unsafe deletion succeeded")
			}
			mock.Lock()
			deletes, remaining := mock.DraftDeletes, len(mock.Drafts)
			mock.Unlock()
			if deletes != 0 || remaining != 1 {
				t.Fatal("submitted unsafe deletion", deletes, remaining)
			}
		})
	}
}

func TestCloudLifecycleMarkdownProvenance(t *testing.T) {
	s, mock := lifecycleFixture(t)
	source := "**rendered**"
	request := SaveDraftRequest{AccountID: "a", Subject: "Markdown", RawSource: source, RenderedSource: &source, EditorMode: "markdown", BodyText: "rendered", BodyHTML: "<p><strong>rendered</strong></p>"}
	id, err := s.Save(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cloud(t.Context(), CloudDraftRequest{AccountID: "a", DraftID: id, Action: "upload"}); err != nil {
		t.Fatal(err)
	}
	request.ExistingDraftID, request.RawSource = id, "new unrendered source"
	if _, err := s.Save(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cloud(t.Context(), CloudDraftRequest{AccountID: "a", DraftID: id, Action: "upload"}); err == nil {
		t.Fatal("stale Markdown rendering uploaded")
	}
	mock.Lock()
	writes := mock.DraftWrites
	mock.Unlock()
	if writes != 1 {
		t.Fatal("stale source reached Gmail")
	}
}
