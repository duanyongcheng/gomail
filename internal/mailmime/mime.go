package mailmime

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"time"

	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
)

var ErrMessageTooLarge = errors.New("message exceeds size limit")

func Parse(raw []byte) (Message, error) {
	return ParseWithLimit(raw, MaxMessageBytes)
}

// ParseWithLimit bounds both the wire message and total decoded MIME data.
// Protocol prototypes retain Parse's smaller default limit.
func ParseWithLimit(raw []byte, limit int) (Message, error) {
	return parseWithLimit(raw, limit, false)
}

// Drafts may not have a Date or Message-ID until sent. Their absent headers
// remain deterministic zero values for content comparison, not invented dates.
func ParseDraftWithLimit(raw []byte, limit int) (Message, error) {
	return parseWithLimit(raw, limit, true)
}

func parseWithLimit(raw []byte, limit int, draft bool) (Message, error) {
	var out Message
	if limit <= 0 {
		return out, errors.New("invalid MIME size limit")
	}
	if len(raw) > limit {
		return out, ErrMessageTooLarge
	}
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return out, err
	}
	defer r.Close()
	if out.Subject, err = r.Header.Subject(); err != nil {
		return out, err
	}
	if !draft || r.Header.Get("Message-ID") != "" {
		if out.MessageID, err = r.Header.MessageID(); err != nil {
			return out, err
		}
	}
	from, err := r.Header.AddressList("From")
	if err != nil || len(from) != 1 {
		return out, errors.New("one valid sender required")
	}
	name := DecodeAddressName(from[0].Name)
	out.From = Address{&name, from[0].Address}
	to, err := r.Header.AddressList("To")
	if err != nil {
		return out, err
	}
	out.To = make([]Address, 0, len(to))
	for _, a := range to {
		n := DecodeAddressName(a.Name)
		out.To = append(out.To, Address{&n, a.Address})
	}
	for _, field := range []struct {
		name   string
		target *[]Address
	}{{"Cc", &out.CC}, {"Bcc", &out.BCC}} {
		addresses, err := r.Header.AddressList(field.name)
		if err != nil {
			return out, err
		}
		*field.target = []Address{}
		for _, a := range addresses {
			name := DecodeAddressName(a.Name)
			*field.target = append(*field.target, Address{Name: &name, Address: a.Address})
		}
	}
	out.InReplyTo = strings.Join(ThreadHeaderIDs(r.Header.Get("In-Reply-To")), " ")
	out.References = strings.Join(ThreadHeaderIDs(r.Header.Get("References")), " ")
	if !draft || r.Header.Get("Date") != "" {
		date, dateErr := r.Header.Date()
		if dateErr != nil {
			return out, dateErr
		}
		out.Date = date.Unix()
	}
	out.Attachments = []Attachment{}
	var textBody, htmlBody strings.Builder
	decoded := 0
	parts := 0
	for {
		part, e := r.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return out, e
		}
		parts++
		if parts > 1000 {
			return out, errors.New("MIME part limit exceeded")
		}
		data, e := io.ReadAll(io.LimitReader(part.Body, int64(limit-decoded)+1))
		if e != nil {
			return out, e
		}
		decoded += len(data)
		if decoded > limit {
			return out, ErrMessageTooLarge
		}
		switch h := part.Header.(type) {
		case *mail.InlineHeader:
			typ, _, e := h.ContentType()
			if e != nil {
				return out, e
			}
			filename, e := (&mail.AttachmentHeader{Header: h.Header}).Filename()
			if e != nil {
				return out, e
			}
			if typ == "text/plain" && filename == "" {
				textBody.Write(data)
			} else if typ == "text/html" && filename == "" {
				htmlBody.Write(data)
			} else {
				out.Attachments = append(out.Attachments, Attachment{Filename: filename, MIMEType: typ, ContentID: strings.Trim(h.Get("Content-ID"), "<>"), Inline: true, Data: data})
			}
		case *mail.AttachmentHeader:
			filename, e := h.Filename()
			if e != nil {
				return out, e
			}
			typ, _, e := h.ContentType()
			if e != nil {
				return out, e
			}
			out.Attachments = append(out.Attachments, Attachment{Filename: filename, MIMEType: typ, ContentID: strings.Trim(h.Get("Content-ID"), "<>"), Data: data})
		}
	}
	out.BodyText, out.BodyHTML = textBody.String(), htmlBody.String()
	text := make([]rune, 0, 150)
	for _, r := range out.BodyText {
		if len(text) == 150 {
			break
		}
		text = append(text, r)
	}
	out.Snippet = string(text)
	return out, nil
}

// Build is the deterministic fixture/protocol-prototype builder. Product
// compose/send code must use BuildOutgoing with explicit identity/time instead.
func Build(from, subject, text, html, messageID string, to, cc []*mail.Address, attachments []Attachment) ([]byte, error) {
	var header mail.Header
	header.SetAddressList("From", []*mail.Address{{Name: "协议测试", Address: from}})
	header.SetAddressList("To", to)
	if len(cc) > 0 {
		header.SetAddressList("Cc", cc)
	}
	header.SetSubject(subject)
	header.SetDate(time.Unix(1700000000, 0))
	header.SetMessageID(messageID)
	var buf bytes.Buffer
	w, err := mail.CreateWriter(&buf, header)
	if err != nil {
		return nil, err
	}
	for typ, body := range map[string]string{"text/plain": text, "text/html": html} {
		if body == "" {
			continue
		}
		var h mail.InlineHeader
		h.SetContentType(typ, map[string]string{"charset": "utf-8"})
		part, e := w.CreateSingleInline(h)
		if e != nil {
			return nil, e
		}
		if _, e = io.WriteString(part, body); e != nil {
			return nil, e
		}
		if e = part.Close(); e != nil {
			return nil, e
		}
	}
	for _, a := range attachments {
		if a.Inline {
			var h mail.InlineHeader
			h.SetContentType(a.MIMEType, nil)
			h.Set("Content-ID", "<"+a.ContentID+">")
			part, e := w.CreateSingleInline(h)
			if e != nil {
				return nil, e
			}
			if _, e = part.Write(a.Data); e != nil {
				return nil, e
			}
			if e = part.Close(); e != nil {
				return nil, e
			}
		} else {
			var h mail.AttachmentHeader
			h.SetFilename(a.Filename)
			h.SetContentType(a.MIMEType, nil)
			part, e := w.CreateAttachment(h)
			if e != nil {
				return nil, e
			}
			if _, e = part.Write(a.Data); e != nil {
				return nil, e
			}
			if e = part.Close(); e != nil {
				return nil, e
			}
		}
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	if buf.Len() > MaxMessageBytes {
		return nil, errors.New("outgoing message exceeds limit")
	}
	return buf.Bytes(), nil
}
