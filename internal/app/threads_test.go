package app

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/provider/gmail"
	"github.com/duanyongcheng/pebble-go/internal/store"
	"github.com/duanyongcheng/pebble-go/internal/testgmail"
	"github.com/emersion/go-message/mail"
)

func TestReplyOutboxUsesAccountScopedParentAndNativeThread(t *testing.T) {
	compose, _ := composeFixture(t)
	r := compose.Runtime
	ctx := t.Context()
	mid, refs, thread := "parent@example.invalid", "root@example.invalid ancestor@example.invalid", "native-thread"
	parent := domain.Message{ID: "a:parent", AccountID: "a", RemoteID: "remote-parent", Subject: "Topic", MessageIDHeader: &mid, ReferencesHeader: &refs, ThreadID: &thread}
	if err := r.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: "a", Messages: []store.CachedMessage{{Message: parent}}}); err != nil {
		t.Fatal(err)
	}
	mock := testgmail.NewMock()
	defer mock.Close()
	mock.ProfileEmail = "a@example.invalid"
	r.InstallProvider("a", gmail.New(r.DB, mock.API(staticToken{"demo-access"}), "a"))
	request := sendRequest()
	request.Subject = "Re: Topic"
	request.ReplyMessageID = parent.ID
	if _, err := (&OutboxService{Runtime: r}).Queue(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, err := r.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	sent := mock.Messages["sent-1"]
	raw := append([]byte{}, mock.SentRaw[0]...)
	mock.Unlock()
	if sent.ThreadID != thread {
		t.Fatal("send lost native thread", sent.ThreadID)
	}
	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	gotReply, _ := reader.Header.MsgIDList("In-Reply-To")
	gotRefs, _ := reader.Header.MsgIDList("References")
	if len(gotReply) != 1 || gotReply[0] != mid || len(gotRefs) != 3 || gotRefs[0] != "root@example.invalid" || gotRefs[2] != mid {
		t.Fatal(gotReply, gotRefs)
	}
	request.AccountID = "b"
	request.ClientKey = "cross-account"
	if _, err := (&OutboxService{Runtime: r}).Queue(ctx, request); err == nil {
		t.Fatal("cross-account reply accepted")
	}
	request.AccountID = "a"
	request.Subject = "Changed topic"
	request.ClientKey = "changed-topic"
	if _, err := (&OutboxService{Runtime: r}).Queue(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, err := r.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	second := mock.Messages["sent-2"]
	mock.Unlock()
	if second.ThreadID == thread {
		t.Fatal("changed subject forced into existing thread")
	}
}
func TestLegacyOutboxPayloadAndThreadServices(t *testing.T) {
	var payload outboxPayload
	if err := json.Unmarshal([]byte(`{"Email":"a@example.invalid","Raw":"eA=="}`), &payload); err != nil || payload.ThreadID != "" {
		t.Fatal(payload, err)
	}
	r, _, db := pendingAppFixture(t)
	service := &ThreadService{Runtime: r}
	rows, err := service.List(t.Context(), ThreadsRequest{FolderID: "a:label:INBOX", Limit: 50})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	messages, err := service.Messages(t.Context(), ThreadRequest{ThreadID: rows[0].ThreadID})
	if err != nil || len(messages) != 1 || !messages[0].Capabilities.ModifyFlags || messages[0].BodyText != "" {
		t.Fatal(messages, err)
	}
	full, err := (&MessageService{Runtime: r}).Get(t.Context(), MessageRequest{MessageID: "a:m"})
	if err != nil || full.ThreadID == nil || *full.ThreadID != rows[0].ThreadID {
		t.Fatal(full, err)
	}
	if _, err := db.PublicThreadID(t.Context(), "missing"); err == nil {
		t.Fatal("missing thread accepted")
	}
}
