package gmail_test

import (
	"path/filepath"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/store"
)

func TestThreadHeadersAndRecipientsSurviveRepeatedGmailSync(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, _ := fixture(t, db, "a")
	mock.Set("m1", []byte("From: sender@example.invalid\r\nTo: to@example.invalid\r\nCc: cc@example.invalid\r\nBcc: hidden@example.invalid\r\nMessage-ID: <reply@example.invalid>\r\nIn-Reply-To: <parent@example.invalid>\r\nReferences: <root@example.invalid> <parent@example.invalid>\r\nSubject: Re: topic\r\n\r\nreply body"), []string{"INBOX", "UNREAD"})
	for range 2 {
		if _, err := p.Sync(t.Context()); err != nil {
			t.Fatal(err)
		}
		m, err := db.Message(t.Context(), "a:m1")
		if err != nil || m.InReplyTo == nil || *m.InReplyTo != "parent@example.invalid" || m.ReferencesHeader == nil || *m.ReferencesHeader != "root@example.invalid parent@example.invalid" || len(m.CC) != 1 || len(m.BCC) != 1 {
			t.Fatal(m, err)
		}
		threads, err := db.Threads(t.Context(), store.ThreadsFilter{FolderIDs: []string{"a:label:INBOX"}})
		if err != nil || len(threads) != 2 {
			t.Fatal(threads, err)
		}
	}
}
