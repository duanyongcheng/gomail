package mailcore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

func addresses(values []string) ([]*mail.Address, error) {
	out := make([]*mail.Address, 0, len(values))
	for _, s := range values {
		if strings.ContainsAny(s, "\r\n") {
			return nil, errors.New("header newline rejected")
		}
		a, err := mail.ParseAddress(s)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (c *Core) Send(ctx context.Context, req SendRequest) (SendResult, error) {
	if req.RequestID == "" || len(req.RequestID) > 100 || strings.ContainsAny(req.RequestID, "\r\n<>@ ") {
		return SendResult{}, errors.New("valid send request identity required")
	}
	if strings.ContainsAny(req.Subject, "\r\n") {
		return SendResult{}, errors.New("subject newline rejected")
	}
	to, err := addresses(req.To)
	if err != nil {
		return SendResult{}, err
	}
	cc, err := addresses(req.Cc)
	if err != nil {
		return SendResult{}, err
	}
	bcc, err := addresses(req.Bcc)
	if err != nil {
		return SendResult{}, err
	}
	all := append(append(append([]*mail.Address{}, to...), cc...), bcc...)
	if len(all) == 0 || len(all) > 20 {
		return SendResult{}, errors.New("1-20 recipients required")
	}
	id := req.RequestID + "@example.invalid"
	raw, err := Build(c.Config.Username, req.Subject, req.BodyText, req.BodyHTML, id, to, cc, nil)
	if err != nil {
		return SendResult{}, err
	}
	if _, err = c.DB.Execute(ctx, "INSERT INTO protocol_send(id,status) VALUES(?,'sending')", req.RequestID); err != nil {
		return SendResult{}, errors.New("send request already recorded; never blindly resend")
	}
	setStatus := func(status string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := c.DB.Execute(ctx, "UPDATE protocol_send SET status=? WHERE id=?", status, req.RequestID)
		return err
	}
	conn, close, err := dial(ctx, c.Config.SMTPAddr, c.Config.TLS)
	if err != nil {
		setStatus("failed_before_data")
		return SendResult{}, err
	}
	defer close()
	client := smtp.NewClient(conn)
	defer client.Close()
	if err = client.Auth(sasl.NewPlainClient("", c.Config.Username, c.Config.Password)); err != nil {
		setStatus("failed_before_data")
		return SendResult{}, err
	}
	if err = client.Mail(c.Config.Username, nil); err != nil {
		setStatus("failed_before_data")
		return SendResult{}, err
	}
	for _, a := range all {
		if err = client.Rcpt(a.Address, nil); err != nil {
			setStatus("failed_before_data")
			return SendResult{}, err
		}
	}
	writer, err := client.Data()
	if err != nil {
		setStatus("failed_before_data")
		return SendResult{}, err
	}
	if _, err = bytes.NewReader(raw).WriteTo(writer); err != nil {
		setStatus("outcome_unknown")
		return SendResult{}, fmt.Errorf("%w: %v", ErrOutcomeUnknown, err)
	}
	if err = writer.Close(); err != nil {
		setStatus("outcome_unknown")
		return SendResult{}, fmt.Errorf("%w: %v", ErrOutcomeUnknown, err)
	}
	if err = setStatus("accepted"); err != nil {
		return SendResult{}, fmt.Errorf("SMTP accepted but local completion failed: never resend request %s: %w", req.RequestID, err)
	}
	return SendResult{MessageID: id, Accepted: true}, nil
}
