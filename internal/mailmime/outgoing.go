package mailmime

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	messageMail "github.com/emersion/go-message/mail"
)

// Outgoing is explicit about identity/time and Bcc policy. SMTP keeps Bcc in
// its envelope; Gmail raw-message submission needs it in the submitted headers.
// The caller owns MessageID generation and must retain it across retries.
type Outgoing struct {
	From                                      mail.Address
	To, CC, BCC                               []mail.Address
	Subject, Text, HTML, MessageID, InReplyTo string
	References                                []string
	Date                                      time.Time
	IncludeBcc                                bool
	Attachments                               []Attachment
}

const MaxOutgoingBytes = 35 << 20

func safeHeader(value string) bool {
	if len(value) > 8192 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validMessageID(value string) bool {
	return value != "" && strings.Contains(value, "@") && !strings.ContainsAny(value, "<> \t\r\n") && safeHeader(value)
}

// BuildOutgoing rejects header injection and bounds the encoded output while
// writing, rather than allocating an unbounded message and checking afterward.
func BuildOutgoing(input Outgoing, limit int) ([]byte, error) {
	return buildOutgoing(input, limit, false)
}

// BuildDraft keeps send validation intact while allowing no recipients in drafts.
func BuildDraft(input Outgoing, limit int) ([]byte, error) {
	return buildOutgoing(input, limit, true)
}

func buildOutgoing(input Outgoing, limit int, draft bool) ([]byte, error) {
	if limit <= 0 || limit > MaxOutgoingBytes {
		return nil, errors.New("invalid outgoing message limit")
	}
	if !utf8.ValidString(input.Text) || !utf8.ValidString(input.HTML) {
		return nil, errors.New("outgoing body must be UTF-8")
	}
	if input.Date.IsZero() || !validMessageID(input.MessageID) || !safeHeader(input.Subject) {
		return nil, errors.New("invalid outgoing message headers")
	}
	if input.InReplyTo != "" && !validMessageID(input.InReplyTo) {
		return nil, errors.New("invalid reply message id")
	}
	if len(input.References) > 100 || len(input.To)+len(input.CC)+len(input.BCC) > 1000 || len(input.Attachments) > 100 {
		return nil, errors.New("outgoing message has too many parts or recipients")
	}
	for _, id := range input.References {
		if !validMessageID(id) {
			return nil, errors.New("invalid reference message id")
		}
	}
	convert := func(addresses []mail.Address) ([]*messageMail.Address, error) {
		out := make([]*messageMail.Address, 0, len(addresses))
		for _, a := range addresses {
			if !safeHeader(a.Name) || !safeHeader(a.Address) {
				return nil, errors.New("invalid outgoing address")
			}
			parsed, err := mail.ParseAddress(a.Address)
			if err != nil || parsed.Address != a.Address || !strings.Contains(a.Address, "@") {
				return nil, errors.New("invalid outgoing address")
			}
			out = append(out, &messageMail.Address{Name: a.Name, Address: a.Address})
		}
		return out, nil
	}
	from, err := convert([]mail.Address{input.From})
	if err != nil {
		return nil, err
	}
	to, err := convert(input.To)
	if err != nil {
		return nil, err
	}
	cc, err := convert(input.CC)
	if err != nil {
		return nil, err
	}
	bcc, err := convert(input.BCC)
	if err != nil {
		return nil, err
	}
	if !draft && len(to)+len(cc)+len(bcc) == 0 {
		return nil, errors.New("outgoing recipient required")
	}
	retained := int64(len(input.Text)) + int64(len(input.HTML))
	for _, a := range input.Attachments {
		retained += int64(len(a.Data))
		if !safeHeader(a.Filename) || !safeHeader(a.MIMEType) || !safeHeader(a.ContentID) {
			return nil, errors.New("invalid attachment headers")
		}
		if a.Inline && (a.ContentID == "" || strings.ContainsAny(a.ContentID, "<> \t")) {
			return nil, errors.New("invalid inline content id")
		}
		if _, _, err := mime.ParseMediaType(a.MIMEType); err != nil {
			return nil, errors.New("invalid attachment media type")
		}
	}
	if retained > int64(limit) {
		return nil, ErrMessageTooLarge
	}
	var header messageMail.Header
	header.SetAddressList("From", from)
	header.SetAddressList("To", to)
	if len(cc) > 0 {
		header.SetAddressList("Cc", cc)
	}
	if input.IncludeBcc && len(bcc) > 0 {
		header.SetAddressList("Bcc", bcc)
	}
	header.SetSubject(input.Subject)
	header.SetDate(input.Date)
	header.SetMessageID(input.MessageID)
	if input.InReplyTo != "" {
		header.SetMsgIDList("In-Reply-To", []string{input.InReplyTo})
	}
	if len(input.References) > 0 {
		header.SetMsgIDList("References", input.References)
	}
	buffer := &boundedMIMEBuffer{limit: limit}
	writer, err := messageMail.CreateWriter(buffer, header)
	if err != nil {
		return nil, err
	}
	alternative, err := writer.CreateInline()
	if err != nil {
		return nil, err
	}
	for _, part := range []struct{ kind, body string }{{"text/plain", input.Text}, {"text/html", input.HTML}} {
		if part.kind == "text/html" && part.body == "" {
			continue
		}
		var h messageMail.InlineHeader
		h.SetContentType(part.kind, map[string]string{"charset": "utf-8"})
		w, err := alternative.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if _, err = io.WriteString(w, part.body); err != nil {
			return nil, err
		}
		if err = w.Close(); err != nil {
			return nil, err
		}
	}
	if err = alternative.Close(); err != nil {
		return nil, err
	}
	for _, a := range input.Attachments {
		var w io.WriteCloser
		if a.Inline {
			var h messageMail.InlineHeader
			var params map[string]string
			if a.Filename != "" {
				// go-message overwrites inline Content-Disposition parameters.
				params = map[string]string{"name": a.Filename}
			}
			h.SetContentType(a.MIMEType, params)
			h.Set("Content-ID", "<"+a.ContentID+">")
			w, err = writer.CreateSingleInline(h)
		} else {
			var h messageMail.AttachmentHeader
			h.SetFilename(a.Filename)
			h.SetContentType(a.MIMEType, nil)
			if a.ContentID != "" {
				h.Set("Content-ID", "<"+a.ContentID+">")
			}
			w, err = writer.CreateAttachment(h)
		}
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(a.Data); err != nil {
			return nil, err
		}
		if err = w.Close(); err != nil {
			return nil, err
		}
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

type boundedMIMEBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedMIMEBuffer) WriteString(s string) (int, error) { return b.Write([]byte(s)) }

func (b *boundedMIMEBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, ErrMessageTooLarge
	}
	return b.Buffer.Write(p)
}
