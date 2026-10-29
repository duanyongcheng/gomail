package domain

import (
	"errors"
	"net/mail"
	"strings"
)

type TrustedSender struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	TrustType string `json:"trust_type"`
	CreatedAt int64  `json:"created_at"`
}

// NormalizeTrustedEmail is an exact mailbox preference, not a domain rule or
// identity authentication. This first implementation accepts ASCII addresses
// only and folds case consistently for lookup, update and deletion.
func NormalizeTrustedEmail(value string) (string, error) {
	// Reject controls before trimming; SQL's legacy trim() only removes ASCII
	// spaces, so accepted identities must have the same canonical lookup form.
	for _, r := range value {
		if r < 32 || r >= 127 {
			return "", errors.New("trust requires an ASCII email address without control characters")
		}
	}
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 320 || strings.ContainsAny(value, `<>"(),:;[]\*?`) || strings.Count(value, "@") != 1 {
		return "", errors.New("trust requires one plain email address")
	}
	for _, r := range value {
		if r <= 32 || r >= 127 {
			return "", errors.New("trust requires an ASCII email address without control characters")
		}
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Name != "" || parsed.Address != value {
		return "", errors.New("invalid trusted sender address")
	}
	return strings.ToLower(value), nil
}
