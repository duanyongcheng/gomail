// Package smtp submits frozen Outbox messages once over verified TLS.
package smtp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/mailmime"
	"github.com/duanyongcheng/gomail/internal/mailnet"
)

type Config struct {
	Endpoint                            mailnet.Endpoint
	Dialer                              mailnet.Dialer
	Username, Password, Email, AuthMode string
	Token                               func(context.Context) (string, error)
	BeforeRequest                       func(context.Context) error
}
type Transport struct{ config Config }

func New(c Config) *Transport {
	if c.Dialer.Proxy != nil {
		proxy := *c.Dialer.Proxy
		c.Dialer.Proxy = &proxy
	}
	return &Transport{config: c}
}

type session struct {
	conn       net.Conn
	reader     *bufio.Reader
	stop       func() bool
	extensions string
}

func (s *session) close() { s.stop(); _ = s.conn.Close() }
func (s *session) response() (int, string, error) {
	var text strings.Builder
	code := 0
	for i := 0; i < 100; i++ {
		line, err := s.reader.ReadSlice('\n')
		if err != nil || len(line) < 5 || !bytes.HasSuffix(line, []byte("\r\n")) || text.Len()+len(line) > 32768 {
			return 0, "", errors.New("invalid SMTP response")
		}
		n, err := strconv.Atoi(string(line[:3]))
		if err != nil || n < 200 || n > 599 || (code != 0 && n != code) || (line[3] != ' ' && line[3] != '-') {
			return 0, "", errors.New("invalid SMTP response")
		}
		code = n
		text.Write(line[4:])
		if line[3] == ' ' {
			return code, text.String(), nil
		}
	}
	return 0, "", errors.New("SMTP response limit")
}
func (s *session) command(command string) (int, string, error) {
	if _, err := io.WriteString(s.conn, command+"\r\n"); err != nil {
		return 0, "", err
	}
	return s.response()
}
func (t *Transport) connect(ctx context.Context) (*session, error) {
	c := t.config
	if c.BeforeRequest != nil {
		if err := c.BeforeRequest(ctx); err != nil {
			return nil, err
		}
	}
	if err := c.Endpoint.Validate(); err != nil {
		return nil, err
	}
	if c.Username == "" || len(c.Username) > 512 || len(c.Password) > 4096 || strings.ContainsAny(c.Username, "\x00\r\n\x01") || strings.ContainsAny(c.Password, "\x00\r\n") {
		return nil, errors.New("invalid SMTP credentials")
	}
	var conn net.Conn
	var err error
	if c.Endpoint.Security == "tls" {
		conn, err = c.Dialer.DialTLS(ctx, c.Endpoint)
	} else {
		conn, err = c.Dialer.DialContext(ctx, "tcp", net.JoinHostPort(c.Endpoint.Host, strconv.Itoa(c.Endpoint.Port)))
	}
	if err != nil {
		return nil, err
	}
	s := &session{conn: conn, reader: bufio.NewReaderSize(conn, 8192)}
	s.stop = context.AfterFunc(ctx, func() { _ = conn.Close() })
	setDeadline(conn, ctx, 30*time.Second)
	fail := func() (*session, error) {
		s.close()
		return nil, errors.New("SMTP connection or authentication failed")
	}
	code, _, err := s.response()
	if err != nil || code != 220 {
		return fail()
	}
	code, extensions, err := s.command("EHLO gomail.invalid")
	if err != nil || code != 250 {
		return fail()
	}
	if c.Endpoint.Security == "starttls" {
		if !hasExtension(extensions, "STARTTLS") {
			return fail()
		}
		code, _, err = s.command("STARTTLS")
		if err != nil || code != 220 {
			return fail()
		}
		secure, err := c.Dialer.UpgradeTLS(ctx, conn, c.Endpoint)
		if err != nil {
			return fail()
		}
		s.conn = secure
		setDeadline(secure, ctx, 60*time.Second)
		s.reader = bufio.NewReaderSize(secure, 8192)
		code, extensions, err = s.command("EHLO gomail.invalid")
		if err != nil || code != 250 {
			return fail()
		}
	}
	mode := c.AuthMode
	if mode == "" {
		mode = "password"
	}
	var auth string
	switch mode {
	case "password":
		if !hasAuth(extensions, "PLAIN") {
			return fail()
		}
		auth = "PLAIN " + base64.StdEncoding.EncodeToString([]byte("\x00"+c.Username+"\x00"+c.Password))
	case "xoauth2":
		if !hasAuth(extensions, "XOAUTH2") || c.Token == nil {
			return fail()
		}
		token, err := c.Token(ctx)
		if err != nil {
			s.close()
			return nil, err
		}
		if token == "" || len(token) > 32768 || strings.ContainsAny(token, "\r\n\x00\x01") {
			return fail()
		}
		auth = "XOAUTH2 " + base64.StdEncoding.EncodeToString([]byte("user="+c.Username+"\x01auth=Bearer "+token+"\x01\x01"))
	default:
		return fail()
	}
	code, _, err = s.command("AUTH " + auth)
	if err != nil || code != 235 {
		if err == nil {
			state := domain.ConnectionState("")
			switch code {
			case 535:
				state = domain.ConnectionReauthRequired
			case 534, 538:
				state = domain.ConnectionConfigurationRequired
			}
			if state != "" {
				s.close()
				return nil, &domain.AccessError{State: state}
			}
		}
		return fail()
	}
	s.extensions = extensions
	setDeadline(s.conn, ctx, 60*time.Second)
	return s, nil
}
func hasExtension(text, name string) bool {
	for _, line := range strings.Split(text, "\r\n") {
		f := strings.Fields(line)
		if len(f) > 0 && strings.EqualFold(f[0], name) {
			return true
		}
	}
	return false
}
func hasAuth(text, name string) bool {
	for _, line := range strings.Split(text, "\r\n") {
		f := strings.Fields(line)
		if len(f) > 1 && strings.EqualFold(f[0], "AUTH") {
			for _, m := range f[1:] {
				if strings.EqualFold(m, name) {
					return true
				}
			}
		}
	}
	return false
}
func (t *Transport) Test(ctx context.Context) error {
	s, err := t.connect(ctx)
	if err == nil {
		s.close()
	}
	return err
}
func (t *Transport) Send(ctx context.Context, m domain.OutgoingMail, beforeSend func() error) domain.SendResult {
	fail := func(retry bool) domain.SendResult {
		return domain.SendResult{Retryable: retry, Message: "SMTP submission was not accepted."}
	}
	if !strings.EqualFold(m.Email, t.config.Email) || len(m.Raw) == 0 || len(m.Raw) > mailmime.MaxOutgoingBytes || len(m.EnvelopeRecipients) == 0 || len(m.EnvelopeRecipients) > 1000 {
		return fail(false)
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(m.Raw))
	if err != nil {
		return fail(false)
	}
	from, err := parsed.Header.AddressList("From")
	if err != nil || len(from) != 1 || !strings.EqualFold(from[0].Address, m.Email) {
		return fail(false)
	}
	for header := range parsed.Header {
		if strings.EqualFold(header, "Bcc") || strings.EqualFold(header, "Resent-Bcc") {
			return fail(false)
		}
	}
	sender, err := mail.ParseAddress(m.Email)
	if err != nil || sender.Address != m.Email {
		return fail(false)
	}
	for _, recipient := range m.EnvelopeRecipients {
		a, err := mail.ParseAddress(recipient)
		if err != nil || a.Address != recipient || strings.ContainsAny(recipient, "\r\n<>") {
			return fail(false)
		}
	}
	s, err := t.connect(ctx)
	if err != nil {
		if result, ok := accessFailure(err); ok {
			return result
		}
		return fail(true)
	}
	defer s.close()
	mailCommand := "MAIL FROM:<" + m.Email + ">"
	// International envelope addresses require an explicit SMTPUTF8 offer.
	utf8Envelope := false
	for _, address := range append([]string{m.Email}, m.EnvelopeRecipients...) {
		for _, r := range address {
			if r > 127 {
				utf8Envelope = true
			}
		}
	}
	if utf8Envelope {
		if !hasExtension(s.extensions, "SMTPUTF8") {
			return fail(false)
		}
		mailCommand += " SMTPUTF8"
	}
	code, _, err := s.command(mailCommand)
	if err != nil || code != 250 {
		return fail(err != nil || code/100 == 4)
	}
	for _, recipient := range m.EnvelopeRecipients {
		code, _, err = s.command("RCPT TO:<" + recipient + ">")
		if err != nil || code/100 != 2 {
			return fail(err != nil || code/100 == 4)
		}
	}
	if t.config.BeforeRequest != nil {
		if err := t.config.BeforeRequest(ctx); err != nil {
			if result, ok := accessFailure(err); ok {
				return result
			}
			return fail(false)
		}
	}
	if beforeSend == nil || beforeSend() != nil {
		return domain.SendResult{Retryable: true, Message: "Could not persist send start; no message was submitted."}
	}
	code, _, err = s.command("DATA")
	if err != nil {
		return domain.SendResult{Unknown: true, Message: "SMTP submission outcome is unknown; no retry will be attempted."}
	}
	if code != 354 {
		return fail(code/100 == 4)
	}
	writer := textproto.NewWriter(bufio.NewWriter(s.conn)).DotWriter()
	_, err = writer.Write(m.Raw)
	if err == nil {
		err = writer.Close()
	}
	if err != nil {
		return domain.SendResult{Unknown: true, Message: "SMTP submission outcome is unknown; no retry will be attempted."}
	}
	code, _, err = s.response()
	if err != nil {
		return domain.SendResult{Unknown: true, Message: "SMTP submission outcome is unknown; no retry will be attempted."}
	}
	if code != 250 {
		if code/100 != 4 && code/100 != 5 {
			return domain.SendResult{Unknown: true, Message: "SMTP submission outcome is unknown; no retry will be attempted."}
		}
		return fail(code/100 == 4)
	}
	return domain.SendResult{Accepted: true, Message: "SMTP server accepted the message; recipient delivery is not confirmed."}
}

func accessFailure(err error) (domain.SendResult, bool) {
	var access *domain.AccessError
	if errors.As(err, &access) && access.State != "" && access.State.Blocked() {
		return domain.SendResult{AccessState: access.State, Message: "SMTP authorization requires attention; queued message is retained."}, true
	}
	return domain.SendResult{}, false
}
func (t *Transport) CheckSent(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("SMTP has no authoritative Sent lookup; no resend was attempted")
}

func setDeadline(conn net.Conn, ctx context.Context, duration time.Duration) {
	deadline := time.Now().Add(duration)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	_ = conn.SetDeadline(deadline)
}
