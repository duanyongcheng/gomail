// Package mailcore is an experimental, loopback-only protocol vertical slice.
// It deliberately does not depend on the MyGO desktop API.
package mailcore

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/duanyongcheng/gomail/internal/mailmime"
)

const AccountID = "poc-account"
const MaxMessageBytes = 2 << 20
const MaxSnapshotMessages = 100

var ErrStaleUID = errors.New("UIDVALIDITY changed: synchronize before modifying flags")
var ErrOutcomeUnknown = errors.New("SMTP outcome unknown: do not automatically retry")

type Config struct {
	IMAPAddr, SMTPAddr string
	Username, Password string
	TLS                *tls.Config
}

func validateAddress(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	n, e := strconv.Atoi(port)
	if ip == nil || !ip.IsLoopback() || e != nil || n < 1 || n > 65535 {
		return errors.New("protocol PoC accepts only literal loopback addresses")
	}
	return nil
}
func dial(ctx context.Context, addr string, cfg *tls.Config) (net.Conn, func(), error) {
	if err := validateAddress(addr); err != nil {
		return nil, nil, err
	}
	if cfg == nil || cfg.InsecureSkipVerify {
		return nil, nil, errors.New("verified TLS configuration required")
	}
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}, Config: cfg.Clone()}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	deadline := time.Now().Add(10 * time.Second)
	if t, ok := ctx.Deadline(); ok && t.Before(deadline) {
		deadline = t
	}
	_ = conn.SetDeadline(deadline)
	return conn, func() { stop(); _ = conn.Close() }, nil
}

type Address = mailmime.Address
type Attachment = mailmime.Attachment
type Message = mailmime.Message

func messageID(validity, uid uint32) string {
	return fmt.Sprintf("imap:%s:INBOX:%d:%d", AccountID, validity, uid)
}

type SyncResult struct {
	UIDValidity uint32   `json:"uid_validity"`
	UIDNext     uint32   `json:"uid_next"`
	Count       int      `json:"count"`
	IDs         []string `json:"ids"`
}
type SearchHit struct {
	MessageID   string  `json:"message_id"`
	Score       float64 `json:"score"`
	Snippet     string  `json:"snippet"`
	Subject     string  `json:"subject"`
	FromAddress string  `json:"from_address"`
	Date        int64   `json:"date"`
}
type SendRequest struct {
	RequestID string   `json:"request_id"`
	To        []string `json:"to"`
	Cc        []string `json:"cc"`
	Bcc       []string `json:"bcc"`
	Subject   string   `json:"subject"`
	BodyText  string   `json:"body_text"`
	BodyHTML  string   `json:"body_html"`
}
type SendResult struct {
	MessageID string `json:"message_id"`
	Accepted  bool   `json:"accepted"`
}
