package store

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bary/pebble-go/internal/domain"
)

func TestCachedAddressNamesDecodeAcrossReadersWithoutRewritingStorage(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "profile.sqlite")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.SaveAccount(ctx, domain.Account{ID: "a", Email: "account@example.invalid", Provider: "gmail"}, nil); err != nil {
		t.Fatal(err)
	}
	cached := domain.Message{
		ID: "a:reply", AccountID: "a", RemoteID: "reply", ThreadID: ptr("remote-thread"),
		FromAddress: "sender@example.invalid", FromName: "=?utf-8?B?5q615rC45oiQ?=",
		To: []domain.Address{
			{Name: ptr("=?utf-8?B?aWNlbmVvMDA0?="), Address: "recipient@example.invalid"},
			{Name: nil, Address: "unnamed@example.invalid"},
			{Name: ptr(""), Address: "empty@example.invalid"},
		},
		CC:      []domain.Address{{Name: ptr("=?utf-8?Q?=E5=BC=A0=E4=B8=89?="), Address: "copy@example.invalid"}},
		BCC:     []domain.Address{{Name: ptr("=?GBK?B?wO7LxA==?="), Address: "hidden@example.invalid"}},
		Subject: "Reply", BodyText: "original body", BodyHTML: "<p>original body</p>",
		MessageIDHeader: ptr("reply@example.invalid"), InReplyTo: ptr("parent@example.invalid"),
		ReferencesHeader: ptr("root@example.invalid parent@example.invalid"), IsStarred: true, Date: 1,
	}
	if err := db.CommitSnapshot(ctx, Snapshot{AccountID: "a", Folders: []domain.Folder{{ID: "a-inbox", AccountID: "a", RemoteID: "INBOX", Name: "Inbox", FolderType: "label"}}, Messages: []CachedMessage{{Message: cached, FolderIDs: []string{"a-inbox"}}}}); err != nil {
		t.Fatal(err)
	}

	storedNames := func() []string {
		t.Helper()
		values := make([]string, 4)
		if err := db.read.QueryRowContext(ctx, "SELECT from_name,to_list,cc_list,bcc_list FROM messages WHERE id=?", cached.ID).Scan(&values[0], &values[1], &values[2], &values[3]); err != nil {
			t.Fatal(err)
		}
		return values
	}
	before := storedNames()
	want := cached
	want.FromName = "段永成"
	want.To = append([]domain.Address(nil), cached.To...)
	want.To[0].Name = ptr("iceneo004")
	want.CC = []domain.Address{{Name: ptr("张三"), Address: "copy@example.invalid"}}
	want.BCC = []domain.Address{{Name: ptr("李四"), Address: "hidden@example.invalid"}}

	assertNames := func(t *testing.T, m domain.Message) {
		t.Helper()
		if m.FromName != want.FromName || m.FromAddress != want.FromAddress || !reflect.DeepEqual(m.To, want.To) || !reflect.DeepEqual(m.CC, want.CC) || !reflect.DeepEqual(m.BCC, want.BCC) {
			t.Fatalf("cached names were not decoded without changing addresses: %+v", m)
		}
		if m.ID != cached.ID || m.AccountID != cached.AccountID || m.Subject != cached.Subject || !reflect.DeepEqual(m.MessageIDHeader, cached.MessageIDHeader) || !reflect.DeepEqual(m.InReplyTo, cached.InReplyTo) || !reflect.DeepEqual(m.ReferencesHeader, cached.ReferencesHeader) {
			t.Fatal("identity or reply metadata changed", m)
		}
	}
	for _, phase := range []string{"initial", "after reopen"} {
		t.Run(phase, func(t *testing.T) {
			m, err := db.Message(ctx, cached.ID)
			if err != nil {
				t.Fatal(err)
			}
			assertNames(t, m)
			if m.BodyText != cached.BodyText || m.BodyHTML != cached.BodyHTML {
				t.Fatal("body changed")
			}
			for _, starred := range []bool{false, true} {
				messages, err := db.Messages(ctx, "a-inbox", "a", starred, 50, 0)
				if err != nil || len(messages) != 1 {
					t.Fatal(messages, err)
				}
				assertNames(t, messages[0])
			}
			thread, err := db.PublicThreadID(ctx, cached.ID)
			if err != nil {
				t.Fatal(err)
			}
			messages, err := db.ThreadMessages(ctx, thread)
			if err != nil || len(messages) != 1 {
				t.Fatal(messages, err)
			}
			assertNames(t, messages[0])
			if messages[0].BodyText != "" || messages[0].BodyHTML != "" {
				t.Fatal("thread metadata reader materialized bodies")
			}
			if !reflect.DeepEqual(storedNames(), before) {
				t.Fatal("read-only normalization rewrote stored names")
			}
		})
		if phase == "initial" {
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db, err = Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
