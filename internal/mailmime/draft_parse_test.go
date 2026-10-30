package mailmime

import (
	"bytes"
	"errors"
	"net/mail"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseDraftMissingHeadersAreDeterministic(t *testing.T) {
	raw := []byte("From: Sender <sender@example.invalid>\r\nSubject: unfinished\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nstill composing")
	first, err := ParseDraftWithLimit(raw, len(raw))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ParseDraftWithLimit(raw, len(raw))
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("draft comparison must not invent changing identities or dates", second, err)
	}
	if first.MessageID != "" || first.Date != 0 || first.Subject != "unfinished" || first.BodyText != "still composing" || len(first.To)+len(first.CC)+len(first.BCC) != 0 {
		t.Fatal(first)
	}
	// The received-mail parser previously accepted absent Date as time.Time{}.Unix().
	// Keep that existing behavior separate from draft-specific normalization.
	received, err := ParseWithLimit(raw, len(raw))
	if err != nil || received.MessageID != "" || received.Date != (time.Time{}).Unix() {
		t.Fatal("received-mail parser changed", received, err)
	}
}

func TestParseDraftSuppliedHeadersMatchReceivedMail(t *testing.T) {
	raw := []byte("From: sender@example.invalid\r\nTo: recipient@example.invalid\r\nDate: Thu, 29 Oct 2026 08:00:00 +0000\r\nMessage-ID: <draft@example.invalid>\r\nSubject: saved\r\n\r\nbody")
	draft, err := ParseDraftWithLimit(raw, len(raw))
	if err != nil {
		t.Fatal(err)
	}
	received, err := ParseWithLimit(raw, len(raw))
	if err != nil || !reflect.DeepEqual(received, draft) {
		t.Fatal("draft parser changed complete MIME semantics", received, draft, err)
	}
	if draft.Date != time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC).Unix() || draft.MessageID != "draft@example.invalid" {
		t.Fatal(draft)
	}
}

func TestParseDraftRejectsMalformedSuppliedHeaders(t *testing.T) {
	for _, header := range []string{
		"Date: definitely not a date\r\n",
		"Message-ID: missing-brackets@example.invalid\r\n",
		"Message-ID: <missing-domain>\r\n",
		"Message-ID: <draft@example.invalid\r\n",
	} {
		t.Run(strings.TrimSpace(header), func(t *testing.T) {
			raw := []byte("From: sender@example.invalid\r\n" + header + "\r\nbody")
			if _, err := ParseDraftWithLimit(raw, len(raw)); err == nil {
				t.Fatal("malformed supplied header accepted")
			}
			if _, err := ParseWithLimit(raw, len(raw)); err == nil {
				t.Fatal("received-mail parser no longer rejects malformed header")
			}
		})
	}
}

func completeCloudDraftRaw() []byte {
	return []byte("From: Sender <sender@example.invalid>\r\n" +
		"To: Recipient <recipient@example.invalid>\r\nCc: Copy <copy@example.invalid>\r\nBcc: Hidden <hidden@example.invalid>\r\n" +
		"Subject: cloud draft\r\nIn-Reply-To: <parent@example.invalid>\r\nReferences: <root@example.invalid>\r\n <parent@example.invalid>\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=cloud\r\n\r\n" +
		"--cloud\r\nContent-Type: multipart/alternative; boundary=body\r\n\r\n" +
		"--body\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nplain body\r\n" +
		"--body\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>rich body<img src=\"cid:image-1\"></p>\r\n--body--\r\n" +
		"--cloud\r\nContent-Type: image/png; name=cid-image.png\r\nContent-Disposition: inline; filename=cid-image.png\r\nContent-ID: <image-1>\r\nContent-Transfer-Encoding: base64\r\n\r\nAAECA//+\r\n" +
		"--cloud\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=report.bin\r\nContent-ID: <download-1>\r\nContent-Transfer-Encoding: base64\r\n\r\nAAECAwQF\r\n" +
		"--cloud--\r\n")
}

func TestParseDraftPreservesRecipientsReplyAndAttachmentMetadata(t *testing.T) {
	raw := completeCloudDraftRaw()
	draft, err := ParseDraftWithLimit(raw, len(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.To) != 1 || len(draft.CC) != 1 || len(draft.BCC) != 1 || draft.BCC[0].Address != "hidden@example.invalid" || *draft.BCC[0].Name != "Hidden" {
		t.Fatal("recipient metadata lost", draft)
	}
	if draft.InReplyTo != "parent@example.invalid" || draft.References != "root@example.invalid parent@example.invalid" || draft.BodyText != "plain body" || draft.BodyHTML != `<p>rich body<img src="cid:image-1"></p>` {
		t.Fatal("reply/body metadata lost", draft)
	}
	if len(draft.Attachments) != 2 {
		t.Fatal(draft.Attachments)
	}
	inline, attached := draft.Attachments[0], draft.Attachments[1]
	if !inline.Inline || inline.ContentID != "image-1" || inline.MIMEType != "image/png" || inline.Filename != "cid-image.png" || !bytes.Equal(inline.Data, []byte{0, 1, 2, 3, 255, 254}) {
		t.Fatal("inline image metadata or bytes lost", inline)
	}
	if attached.Inline || attached.ContentID != "download-1" || attached.MIMEType != "application/octet-stream" || attached.Filename != "report.bin" || !bytes.Equal(attached.Data, []byte{0, 1, 2, 3, 4, 5}) {
		t.Fatal("attachment metadata or bytes lost", attached)
	}
}

func TestDraftAttachmentMetadataSurvivesBuildRoundtrip(t *testing.T) {
	attachments := []Attachment{
		{Filename: "cid-image.png", MIMEType: "image/png", ContentID: "image-1", Inline: true, Data: []byte{0, 1, 2, 3, 255, 254}},
		{Filename: "report.bin", MIMEType: "application/octet-stream", ContentID: "download-1", Data: []byte{0, 1, 2, 3, 4, 5}},
	}
	raw, err := BuildDraft(Outgoing{
		From: mail.Address{Address: "sender@example.invalid"}, Subject: "roundtrip",
		MessageID: "saved@example.invalid", Date: time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC),
		Text: "body", HTML: `<p>body<img src="cid:image-1"></p>`, Attachments: attachments,
	}, MaxOutgoingBytes)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseDraftWithLimit(raw, MaxOutgoingBytes)
	if err != nil || !reflect.DeepEqual(parsed.Attachments, attachments) {
		t.Fatal("cloud attachment metadata changed during re-upload", parsed.Attachments, err)
	}
}

func TestParseDraftRetainsNamedInlineTextAsAttachment(t *testing.T) {
	for _, typ := range []string{"text/plain", "text/html"} {
		t.Run(typ, func(t *testing.T) {
			raw := []byte("From: sender@example.invalid\r\nContent-Type: multipart/mixed; boundary=p\r\n\r\n" +
				"--p\r\nContent-Type: text/plain\r\n\r\nactual body\r\n" +
				"--p\r\nContent-Type: " + typ + "; name=notes.txt\r\nContent-Disposition: inline; filename=notes.txt\r\nContent-ID: <notes-1>\r\n\r\nprivate attachment bytes\r\n--p--\r\n")
			parsed, err := ParseDraftWithLimit(raw, len(raw))
			if err != nil || parsed.BodyText != "actual body" || parsed.BodyHTML != "" || len(parsed.Attachments) != 1 {
				t.Fatal("named inline text was folded into the editable body", parsed, err)
			}
			attachment := parsed.Attachments[0]
			if attachment.Filename != "notes.txt" || attachment.MIMEType != typ || attachment.ContentID != "notes-1" || !attachment.Inline || string(attachment.Data) != "private attachment bytes" {
				t.Fatal("named inline text metadata changed", attachment)
			}
		})
	}
}

func TestParseDraftEnforcesWireDecodedAndPartLimits(t *testing.T) {
	raw := completeCloudDraftRaw()
	if _, err := ParseDraftWithLimit(raw, len(raw)-1); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatal("wire budget ignored", err)
	}
	for _, limit := range []int{0, -1} {
		if _, err := ParseDraftWithLimit(raw, limit); err == nil {
			t.Fatal("invalid budget accepted", limit)
		}
	}
	part := "--p\r\nContent-Type: text/plain; charset=iso-8859-1\r\n\r\n" + strings.Repeat("\xff", 300) + "\r\n"
	expanded := []byte("From: sender@example.invalid\r\nContent-Type: multipart/mixed; boundary=p\r\n\r\n" + part + part + "--p--\r\n")
	if _, err := ParseDraftWithLimit(expanded, len(expanded)); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatal("decoded charset expansion budget ignored", err)
	}
	if _, err := ParseDraftWithLimit(expanded, 2*len(expanded)); err != nil {
		t.Fatal("valid bounded expansion rejected", err)
	}
	tooMany := []byte("From: sender@example.invalid\r\nContent-Type: multipart/mixed; boundary=p\r\n\r\n" + strings.Repeat("--p\r\nContent-Type: text/plain\r\n\r\nx\r\n", 1001) + "--p--\r\n")
	if _, err := ParseDraftWithLimit(tooMany, len(tooMany)); err == nil {
		t.Fatal("part budget ignored")
	}
}

func TestParseDraftRejectsMalformedMIMEAndSender(t *testing.T) {
	for _, raw := range []string{
		"not a header\r\n\r\nbody",
		"Subject: missing sender\r\n\r\nbody",
		"From: first@example.invalid,second@example.invalid\r\n\r\nbody",
		"From: sender@example.invalid\r\nContent-Type: multipart/mixed; boundary=missing\r\n\r\n",
		"From: sender@example.invalid\r\nContent-Transfer-Encoding: base64\r\n\r\n!!!!",
	} {
		t.Run(raw[:min(40, len(raw))], func(t *testing.T) {
			if _, err := ParseDraftWithLimit([]byte(raw), len(raw)); err == nil {
				t.Fatal("malformed cloud MIME accepted")
			}
		})
	}
}
