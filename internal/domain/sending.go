package domain

import "context"

type OutgoingMail struct {
	Email    string
	Raw      []byte
	ThreadID string // Native provider thread, not the UI's account-scoped handle.
}

type SendResult struct {
	RemoteID  string
	Unknown   bool
	Retryable bool
	Message   string
}

// SendTransport calls beforeSend after read-only identity validation and before
// any non-idempotent I/O. All uncertain post-submission errors set Unknown.
type SendTransport interface {
	Send(context.Context, OutgoingMail, func() error) SendResult
	CheckSent(context.Context, string, string) (string, error)
}
