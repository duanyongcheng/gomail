package mailmime

import (
	"bytes"
	"errors"
	"net/mail"
	"strings"
	"testing"
	"time"

	messageMail "github.com/emersion/go-message/mail"
)

func outgoingFixture() Outgoing {
	return Outgoing{From: mail.Address{Name: "真实发件人", Address: "sender@example.invalid"}, To: []mail.Address{{Address: "to@example.invalid"}}, CC: []mail.Address{{Address: "cc@example.invalid"}}, BCC: []mail.Address{{Address: "hidden@example.invalid"}}, Subject: "中文主题", Text: "正文", HTML: "<p>正文</p>", MessageID: "stable@example.invalid", InReplyTo: "original@example.invalid", References: []string{"root@example.invalid", "original@example.invalid"}, Date: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), Attachments: []Attachment{{Filename: "测试.txt", MIMEType: "text/plain", Data: []byte("附件")}}}
}
func TestBuildOutgoingRoundTripAndBccPolicy(t *testing.T) {
	for _, bcc := range []bool{false, true} {
		input := outgoingFixture()
		input.IncludeBcc = bcc
		raw, err := BuildOutgoing(input, MaxOutgoingBytes)
		if err != nil {
			t.Fatal(err)
		}
		r, err := messageMail.CreateReader(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		from, _ := r.Header.AddressList("From")
		date, _ := r.Header.Date()
		hidden, _ := r.Header.AddressList("Bcc")
		reply, _ := r.Header.MsgIDList("In-Reply-To")
		refs, _ := r.Header.MsgIDList("References")
		r.Close()
		if len(from) != 1 || from[0].Name != input.From.Name || !date.Equal(input.Date) || (len(hidden) > 0) != bcc || len(reply) != 1 || reply[0] != input.InReplyTo || len(refs) != 2 {
			t.Fatal(from, date, hidden, reply, refs)
		}
		if !strings.Contains(string(raw), "multipart/alternative") || strings.Contains(string(raw), "协议测试") {
			t.Fatal("fixture headers or wrong MIME structure")
		}
		parsed, err := ParseWithLimit(raw, MaxOutgoingBytes)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Subject != input.Subject || parsed.BodyText != input.Text || parsed.BodyHTML != input.HTML || len(parsed.Attachments) != 1 || parsed.Attachments[0].Filename != "测试.txt" || string(parsed.Attachments[0].Data) != "附件" {
			t.Fatal(parsed)
		}
	}
}
func TestBuildOutgoingRejectsInjectionAndBounds(t *testing.T) {
	for _, mutate := range []func(*Outgoing){func(m *Outgoing) { m.Subject = "ok\r\nBcc: injected@example.invalid" }, func(m *Outgoing) { m.From.Name = "sender\nX: header" }, func(m *Outgoing) { m.MessageID = "bad>\r\nX:a" }, func(m *Outgoing) { m.InReplyTo = "bad <id>" }, func(m *Outgoing) { m.Attachments[0].Filename = "file\nInjected" }} {
		m := outgoingFixture()
		mutate(&m)
		if _, err := BuildOutgoing(m, MaxOutgoingBytes); err == nil {
			t.Fatal("injection accepted")
		}
	}
	m := outgoingFixture()
	m.Text = strings.Repeat("=", 2000)
	m.HTML = ""
	m.Attachments = nil
	if _, err := BuildOutgoing(m, 3000); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatal("encoded bound ignored", err)
	}
	m = outgoingFixture()
	m.Text = strings.Repeat("x", 4000)
	if _, err := BuildOutgoing(m, 2000); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatal("input bound ignored", err)
	}
	m = outgoingFixture()
	m.To = nil
	m.CC = nil
	m.BCC = nil
	if _, err := BuildOutgoing(m, MaxOutgoingBytes); err == nil {
		t.Fatal("missing recipient accepted")
	}
}
