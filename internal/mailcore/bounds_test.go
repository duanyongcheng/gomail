package mailcore

import (
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-message/mail"
)

func TestMalformedMIMEAndBoundsKeepPreviousSnapshot(t *testing.T) {
	c, s := setup(t)
	_, _ = s.Append(fixture(t, "Good", "valid body", "good@example.invalid"))
	initial, err := c.Sync(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	// Raw wire data independent of the message writer, with a deliberately
	// unsupported charset. A parser error must not hide good cached messages.
	bad := []byte("From: sender@example.invalid\r\nTo: poc@example.invalid\r\nSubject: Bad charset\r\nDate: Tue, 14 Nov 2023 22:13:20 +0000\r\nMessage-ID: <bad@example.invalid>\r\nContent-Type: text/plain; charset=does-not-exist\r\n\r\ninvalid\r\n")
	_, _ = s.Append(bad)
	if _, err = c.Sync(ctx(t)); err == nil {
		t.Fatal("unknown charset silently accepted")
	}
	cursor, _ := c.Cursor(ctx(t))
	if cursor.UIDNext != initial.UIDNext || count(t, c, "SELECT count(*) FROM messages WHERE is_deleted=0") != 1 {
		t.Fatal("parser failure advanced snapshot", cursor)
	}
	if _, err = Parse(make([]byte, MaxMessageBytes+1)); err == nil {
		t.Fatal("oversized body accepted")
	}
}

func TestRFC2047RawMessageAndInlineAttachment(t *testing.T) {
	// Encoded-word subject for 中文, quoted-printable body for 中文正文.
	raw := []byte("From: =?UTF-8?B?5Lit5paH?= <sender@example.invalid>\r\nTo: poc@example.invalid\r\nSubject: =?UTF-8?B?5Lit5paH?=\r\nDate: Tue, 14 Nov 2023 22:13:20 +0000\r\nMessage-ID: <raw@example.invalid>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n=E4=B8=AD=E6=96=87=E6=AD=A3=E6=96=87\r\n")
	m, err := Parse(raw)
	if err != nil || m.Subject != "中文" || !strings.Contains(m.BodyText, "中文正文") || m.From.Name == nil || *m.From.Name != "中文" {
		t.Fatal(m, err)
	}
	data := []byte{0, 1, 2, 3, 255}
	encoded, err := Build("sender@example.invalid", "inline", "text", "<img src=\"cid:part\">", "inline@example.invalid", []*mail.Address{{Address: "poc@example.invalid"}}, nil, []Attachment{{ContentID: "part", Inline: true, MIMEType: "image/png", Data: data}})
	if err != nil {
		t.Fatal(err)
	}
	m, err = Parse(encoded)
	if err != nil || len(m.Attachments) != 1 || !m.Attachments[0].Inline || m.Attachments[0].ContentID != "part" || string(m.Attachments[0].Data) != string(data) {
		t.Fatal(m, err)
	}
}

func TestSnapshotLimitAndEscapedSearch(t *testing.T) {
	c, s := setup(t)
	for i := 0; i <= MaxSnapshotMessages; i++ {
		_, err := s.Append(fixture(t, "Bound", "hello", "bound@example.invalid"))
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Sync(ctx(t)); err == nil {
		t.Fatal("unbounded snapshot accepted")
	}
	if count(t, c, "SELECT count(*) FROM protocol_cursor") != 0 {
		t.Fatal("bound advanced cursor")
	}
	if _, err := c.Search(ctx(t), strings.Repeat("a", 513), AccountID, 10); err == nil {
		t.Fatal("unbounded query accepted")
	}
	for _, q := range []string{"%", "_", "\" OR \"", "' OR 1=1 --"} {
		hits, err := c.Search(ctx(t), q, AccountID, 10)
		if err != nil || len(hits) != 0 {
			t.Fatal(q, hits, err)
		}
	}
}

func TestIMAPDisconnectKeepsCursor(t *testing.T) {
	c, s := setup(t)
	_, _ = s.Append(fixture(t, "first", "cached", "disconnect-one@example.invalid"))
	initial, err := c.Sync(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Append(fixture(t, "second", "later", "disconnect-two@example.invalid"))
	s.DropFetch.Store(true)
	if _, err = c.Sync(ctx(t)); err == nil {
		t.Fatal("disconnect ignored")
	}
	cursor, _ := c.Cursor(ctx(t))
	if cursor.UIDNext != initial.UIDNext || count(t, c, "SELECT count(*) FROM messages WHERE is_deleted=0") != 1 {
		t.Fatal("disconnected fetch advanced cursor", cursor)
	}
	if result, err := c.Sync(ctx(t)); err != nil || result.Count != 2 {
		t.Fatal("reconnect recovery", result, err)
	}
}

func TestSMTPFinalReplyLostRequestPersists(t *testing.T) {
	c, s := setup(t)
	s.DropSMTPReply.Store(true)
	r := SendRequest{RequestID: "persisted", To: []string{"recipient@example.invalid"}, Subject: "persist", BodyText: "test"}
	if _, err := c.Send(ctx(t), r); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	// Reconstruct the service over the same DB: no in-memory dedupe is needed.
	second, err := New(ctx(t), c.DB, c.Config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = second.Send(ctx(t), r); err == nil || len(s.Captures()) != 1 {
		t.Fatal("service restart re-sent unknown result", err)
	}
}
