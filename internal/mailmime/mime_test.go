package mailmime

import (
	"errors"
	"strings"
	"testing"
)

func TestExplicitMIMELimitSupportsLargeMessagesWithoutChangingPrototypeDefault(t *testing.T) {
	raw := []byte("From: sender@example.invalid\r\nSubject: large\r\n\r\n" + strings.Repeat("x", MaxMessageBytes))
	if _, err := Parse(raw); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatal("default limit changed", err)
	}
	message, err := ParseWithLimit(raw, len(raw))
	if err != nil || len(message.BodyText) != MaxMessageBytes || len(message.Snippet) != 150 {
		t.Fatal("explicit limit failed", err)
	}
	if _, err := ParseWithLimit(raw, len(raw)-1); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatal("wire limit ignored", err)
	}
}

func TestDecodedBudgetCoversAllPartsAndCharsetExpansion(t *testing.T) {
	part := "--p\r\nContent-Type: text/plain; charset=iso-8859-1\r\n\r\n" + strings.Repeat("\xff", 300) + "\r\n"
	raw := []byte("From: sender@example.invalid\r\nContent-Type: multipart/mixed; boundary=p\r\n\r\n" + part + part + "--p--\r\n")
	if _, err := ParseWithLimit(raw, len(raw)); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatal("total decoded limit ignored", err)
	}
	if _, err := ParseWithLimit(raw, 2*len(raw)); err != nil {
		t.Fatal("valid expanded MIME rejected", err)
	}
}

func TestMIMEPartCountAndUnicodeSnippet(t *testing.T) {
	raw := []byte("From: sender@example.invalid\r\nContent-Type: multipart/mixed; boundary=p\r\n\r\n" + strings.Repeat("--p\r\nContent-Type: text/plain\r\n\r\nx\r\n", 1001) + "--p--\r\n")
	if _, err := Parse(raw); err == nil {
		t.Fatal("unbounded MIME parts accepted")
	}
	message, err := Parse([]byte("From: sender@example.invalid\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + strings.Repeat("字", 200)))
	if err != nil || len([]rune(message.Snippet)) != 150 {
		t.Fatal("snippet split unicode", message.Snippet, err)
	}
}
