package app

import (
	"bytes"
	"database/sql"
	"errors"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/duanyongcheng/gomail/internal/mailmime"
	"github.com/duanyongcheng/gomail/internal/provider/gmail"
	"github.com/duanyongcheng/gomail/internal/store"
	"github.com/duanyongcheng/gomail/internal/testgmail"
)

func cloudOutboxFixture(t *testing.T) (*ComposeService, *testgmail.Mock, SendRequest) {
	t.Helper()
	compose, _ := composeFixture(t)
	mock := testgmail.NewMock()
	t.Cleanup(mock.Close)
	mock.ProfileEmail = "a@example.invalid"
	compose.Runtime.InstallProvider("a", gmail.New(compose.Runtime.DB, mock.API(staticToken{"demo-access"}), "a"))
	local, err := compose.Save(t.Context(), SaveDraftRequest{AccountID: "a", ClientKey: "bound-draft", Subject: "cloud snapshot", BodyText: "original cloud", EditorMode: "rich", To: []string{"to@example.invalid"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compose.Cloud(t.Context(), CloudDraftRequest{AccountID: "a", DraftID: local, Action: "upload"}); err != nil {
		t.Fatal(err)
	}
	request := sendRequest()
	request.LocalDraftID = local
	return compose, mock, request
}

func TestCloudOutboxSendsImmutableContentAndConsumesOnlyBoundDraft(t *testing.T) {
	compose, mock, request := cloudOutboxFixture(t)
	r, ctx := compose.Runtime, t.Context()
	outbox := &OutboxService{Runtime: r}
	if _, err := outbox.Queue(ctx, request); err != nil {
		t.Fatal(err)
	}
	items, _ := r.DB.OutboxList(ctx, "a")
	row, _ := r.DB.Outbox(ctx, "a", items[0].ID)
	payload, err := r.decodeOutbox(ctx, row)
	if err != nil || payload.CloudDraft == nil {
		t.Fatal(payload, err)
	}
	if bytes.Contains(row.Data, []byte(payload.CloudDraft.RemoteID)) || bytes.Contains(row.Data, []byte(request.Subject)) {
		t.Fatal("unencrypted send identity")
	}
	second := request
	second.ClientKey = "second-window"
	if _, err := outbox.Queue(ctx, second); err == nil {
		t.Fatal("second compose reserved same remote draft")
	}
	if _, err := compose.Delete(ctx, DraftRequest{AccountID: "a", DraftID: request.LocalDraftID}); err != nil {
		t.Fatal("post-queue local cleanup lost", err)
	}
	if _, err := outbox.Queue(ctx, request); err != nil {
		t.Fatal("idempotent lost-response retry failed after local cleanup", err)
	}
	if _, err := r.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	delivered, remaining, draftSends := append([][]byte{}, mock.SentRaw...), len(mock.Drafts), mock.DraftSends
	requests := append([]string{}, mock.Requests...)
	mock.Unlock()
	if len(delivered) != 1 || remaining != 0 || draftSends != 1 {
		t.Fatal(len(delivered), remaining, draftSends)
	}
	for _, path := range requests {
		if strings.HasSuffix(path, "/messages/send") {
			t.Fatal("linked draft fell back to messages.send")
		}
	}
	parsed, err := mailmime.ParseWithLimit(delivered[0], mailmime.MaxOutgoingBytes)
	if err != nil || parsed.Subject != request.Subject || parsed.BodyText != request.BodyText || len(parsed.BCC) != 1 {
		t.Fatal(parsed, err)
	}
	copy, _ := r.DB.GmailDraftCopy(ctx, "a", request.LocalDraftID)
	row, _ = r.DB.Outbox(ctx, "a", items[0].ID)
	if copy.State != "sent" || row.Status != "sent" {
		t.Fatal(copy.State, row.Status)
	}
	if _, err := r.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	count := mock.DraftSends
	mock.Unlock()
	if count != 1 {
		t.Fatal("resent cloud draft")
	}
}

func TestCloudOutboxPreflightRefusesChangedOrMissingDraftWithoutFallback(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed", true: "missing"}[missing], func(t *testing.T) {
			compose, mock, request := cloudOutboxFixture(t)
			r, ctx := compose.Runtime, t.Context()
			if _, err := (&OutboxService{Runtime: r}).Queue(ctx, request); err != nil {
				t.Fatal(err)
			}
			copy, _ := r.DB.GmailDraftCopy(ctx, "a", request.LocalDraftID)
			mock.Lock()
			if missing {
				delete(mock.Drafts, copy.RemoteID)
			} else {
				remote := mock.Drafts[copy.RemoteID]
				remote.Message.ID = "external-edit"
				mock.Drafts[copy.RemoteID] = remote
			}
			mock.Unlock()
			for range 2 {
				if _, err := r.processOutbox(ctx); err != nil {
					t.Fatal(err)
				}
			}
			items, _ := r.DB.OutboxList(ctx, "a")
			if len(items) != 1 || items[0].Status != "failed" || items[0].Retryable {
				t.Fatal(items)
			}
			mock.Lock()
			sent := len(mock.SentRaw)
			requests := append([]string{}, mock.Requests...)
			mock.Unlock()
			if sent != 0 {
				t.Fatal("sent an unverified draft")
			}
			for _, path := range requests {
				if strings.HasSuffix(path, "/drafts/send") || strings.HasSuffix(path, "/messages/send") {
					t.Fatal("submitted despite preflight conflict", path)
				}
			}
		})
	}
}

func TestCloudOutboxUnknownReopensAndCheckSentSettlesCloudTogether(t *testing.T) {
	compose, mock, request := cloudOutboxFixture(t)
	r, ctx := compose.Runtime, t.Context()
	mock.Lock()
	mock.LoseSendResponse = true
	mock.Unlock()
	if _, err := (&OutboxService{Runtime: r}).Queue(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, err := r.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	items, _ := r.DB.OutboxList(ctx, "a")
	if len(items) != 1 || items[0].Status != "outcome_unknown" {
		t.Fatal(items)
	}
	db, err := store.Open(ctx, r.DB.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	next := New(ctx, db, r.Keyring, nil)
	defer next.Scheduler.Close()
	next.InstallProvider("a", gmail.New(db, mock.API(staticToken{"demo-access"}), "a"))
	if err := db.RecoverOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := next.processOutbox(ctx); err != nil {
			t.Fatal(err)
		}
	}
	service := &OutboxService{Runtime: next}
	if _, err := service.Action(ctx, OutboxActionRequest{AccountID: "a", ID: items[0].ID, Action: "dismiss"}); err == nil {
		t.Fatal("dismiss destroyed unresolved send")
	}
	if _, err := service.Action(ctx, OutboxActionRequest{AccountID: "a", ID: items[0].ID, Action: "check"}); err != nil {
		t.Fatal(err)
	}
	copy, _ := db.GmailDraftCopy(ctx, "a", request.LocalDraftID)
	row, _ := db.Outbox(ctx, "a", items[0].ID)
	if copy.State != "sent" || row.Status != "sent" {
		t.Fatal(copy.State, row.Status)
	}
	mock.Lock()
	sends, remaining := mock.DraftSends, len(mock.Drafts)
	mock.Unlock()
	if sends != 1 || remaining != 0 {
		t.Fatal(sends, remaining)
	}
}

func TestCloudOutboxLocalBarriersAndReceiptFailureDoNotResend(t *testing.T) {
	compose, mock, request := cloudOutboxFixture(t)
	r, ctx := compose.Runtime, t.Context()
	if _, err := (&OutboxService{Runtime: r}).Queue(ctx, request); err != nil {
		t.Fatal(err)
	}
	injectPendingSQL(t, r.DB, `CREATE TRIGGER cloud_block_start BEFORE UPDATE OF status ON outbox WHEN NEW.status='in_progress' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := r.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	sends, remaining := mock.DraftSends, len(mock.Drafts)
	mock.Unlock()
	if sends != 0 || remaining != 1 {
		t.Fatal("sent without durable start", sends, remaining)
	}
	injectPendingSQL(t, r.DB, "DROP TRIGGER cloud_block_start")
	items, _ := r.DB.OutboxList(ctx, "a")
	if _, err := (&OutboxService{Runtime: r}).Action(ctx, OutboxActionRequest{AccountID: "a", ID: items[0].ID, Action: "retry"}); err != nil {
		t.Fatal(err)
	}
	injectPendingSQL(t, r.DB, `CREATE TRIGGER cloud_block_receipt BEFORE UPDATE OF state ON gmail_draft_copies WHEN NEW.state='sent' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	for range 2 {
		if _, err := r.processOutbox(ctx); err == nil {
			t.Fatal("receipt failure swallowed")
		}
	}
	mock.Lock()
	sends = mock.DraftSends
	mock.Unlock()
	if sends != 1 {
		t.Fatal("receipt failure repeated remote send", sends)
	}
	injectPendingSQL(t, r.DB, "DROP TRIGGER cloud_block_receipt")
	if _, err := r.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	copy, _ := r.DB.GmailDraftCopy(ctx, "a", request.LocalDraftID)
	if copy.State != "sent" {
		t.Fatal(copy.State)
	}
}

func TestCloudOutboxCancelPreservesRemoteAndReleasesReservation(t *testing.T) {
	compose, mock, request := cloudOutboxFixture(t)
	r, ctx := compose.Runtime, t.Context()
	outbox := &OutboxService{Runtime: r}
	wrong := request
	wrong.AccountID = "b"
	if _, err := outbox.Queue(ctx, wrong); err == nil {
		t.Fatal("cross-account local draft accepted")
	}
	if _, err := outbox.Queue(ctx, request); err != nil {
		t.Fatal(err)
	}
	items, _ := r.DB.OutboxList(ctx, "a")
	if _, err := outbox.Action(ctx, OutboxActionRequest{AccountID: "a", ID: items[0].ID, Action: "cancel"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DB.ClaimOutbox(ctx, time.Now().Add(time.Hour).Unix()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cancelled claimed", err)
	}
	copy, _ := r.DB.GmailDraftCopy(ctx, "a", request.LocalDraftID)
	mock.Lock()
	remaining, sends := len(mock.Drafts), mock.DraftSends
	mock.Unlock()
	if copy.State != "synced" || copy.OutboxID != "" || remaining != 1 || sends != 0 {
		t.Fatal(copy, remaining, sends)
	}
}

func TestCloudOutboxImportedReferencesInlinePartsAndThreadSubject(t *testing.T) {
	for _, changedSubject := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-subject", true: "changed-subject"}[changedSubject], func(t *testing.T) {
			compose, _ := composeFixture(t)
			r, ctx := compose.Runtime, t.Context()
			mock := testgmail.NewMock()
			defer mock.Close()
			mock.ProfileEmail = "a@example.invalid"
			r.InstallProvider("a", gmail.New(r.DB, mock.API(staticToken{"demo-access"}), "a"))
			raw, err := mailmime.BuildDraft(mailmime.Outgoing{
				From: mail.Address{Address: "a@example.invalid"}, To: []mail.Address{{Address: "to@example.invalid"}},
				BCC: []mail.Address{{Address: "hidden@example.invalid"}}, IncludeBcc: true,
				Subject: "Conversation", Text: "inline body", HTML: `<p>inline body<img src="cid:image-part"></p>`,
				Date: time.Unix(1700000000, 0), MessageID: "imported@example.invalid",
				InReplyTo: "parent@example.invalid", References: []string{"ancestor@example.invalid", "parent@example.invalid"},
				Attachments: []mailmime.Attachment{{Filename: "image.svg", MIMEType: "image/svg+xml", ContentID: "image-part", Inline: true, Data: []byte("<svg/>")}},
			}, mailmime.MaxOutgoingBytes)
			if err != nil {
				t.Fatal(err)
			}
			// Web drafts can omit Date/Message-ID until sent. The bound remote
			// identity, not an invented RFC header, must authorize this send.
			parts := strings.SplitN(string(raw), "\r\n\r\n", 2)
			headers := []string{}
			for _, line := range strings.Split(parts[0], "\r\n") {
				lower := strings.ToLower(line)
				if !strings.HasPrefix(lower, "date:") && !strings.HasPrefix(lower, "message-id:") {
					headers = append(headers, line)
				}
			}
			raw = []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + parts[1])
			messageID := mock.SetDraft("web-draft", raw, "original-thread")
			local, err := compose.CloudImport(ctx, CloudDraftImportRequest{AccountID: "a", RemoteDraftID: "web-draft", ExpectedMessageID: messageID})
			if err != nil {
				t.Fatal(err)
			}
			request := sendRequest()
			request.LocalDraftID, request.Subject = local.ID, local.Subject
			request.InReplyTo, request.BodyText, request.BodyHTML = local.InReplyTo, local.BodyText, local.BodyHTML
			for _, attachment := range local.Attachments {
				request.AttachmentPaths = append(request.AttachmentPaths, attachment.Path)
			}
			if changedSubject {
				request.Subject = "A different conversation"
			}
			if _, err := (&OutboxService{Runtime: r}).Queue(ctx, request); err != nil {
				t.Fatal(err)
			}
			items, _ := r.DB.OutboxList(ctx, "a")
			record, _ := r.DB.Outbox(ctx, "a", items[0].ID)
			payload, err := r.decodeOutbox(ctx, record)
			if err != nil {
				t.Fatal(err)
			}
			if (!changedSubject && payload.ThreadID != "original-thread") || (changedSubject && payload.ThreadID != "") {
				t.Fatal("wrong native thread", payload.ThreadID)
			}
			parsed, err := mailmime.ParseWithLimit(payload.Raw, mailmime.MaxOutgoingBytes)
			if err != nil || parsed.InReplyTo != "parent@example.invalid" || !strings.Contains(parsed.References, "ancestor@example.invalid") || len(parsed.Attachments) != 1 {
				t.Fatal(parsed, err)
			}
			part := parsed.Attachments[0]
			if !part.Inline || part.MIMEType != "image/svg+xml" || part.ContentID != "image-part" || !bytes.Equal(part.Data, []byte("<svg/>")) {
				t.Fatal("imported attachment metadata lost", part)
			}
		})
	}
}
