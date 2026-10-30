package app

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bary/pebble-go/internal/store"
)

const signaturePurpose = "email_signatures.data"
const maxSignatureBytes = 64 << 10

type UpdateAccountRequest struct {
	AccountID          string  `json:"accountId"`
	Email              string  `json:"email"`
	DisplayName        string  `json:"displayName"`
	AccountColor       *string `json:"accountColor,omitempty"`
	AccountLabel       *string `json:"accountLabel,omitempty"`
	Signature          *string `json:"signature,omitempty"`
	Password           *string `json:"password,omitempty"`
	ImapHost           *string `json:"imapHost,omitempty"`
	ImapPort           *int    `json:"imapPort,omitempty"`
	SmtpHost           *string `json:"smtpHost,omitempty"`
	SmtpPort           *int    `json:"smtpPort,omitempty"`
	ImapSecurity       *string `json:"imapSecurity,omitempty"`
	SmtpSecurity       *string `json:"smtpSecurity,omitempty"`
	AcceptInvalidCerts *bool   `json:"acceptInvalidCerts,omitempty"`
	ProxyHost          *string `json:"proxyHost,omitempty"`
	ProxyPort          *int    `json:"proxyPort,omitempty"`
}

type SignatureRequest struct {
	AccountID string `json:"accountId"`
	Signature string `json:"signature"`
}

func validAccountText(value string, limit int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit && !strings.ContainsFunc(value, unicode.IsControl)
}
func validSignature(value string) bool {
	return utf8.ValidString(value) && len(value) <= maxSignatureBytes && !strings.ContainsRune(value, 0)
}

// Update can be used offline and with expired authorization. No network or
// credential mutation is needed to change local account preferences.
func (s *AccountService) Update(ctx context.Context, request UpdateAccountRequest) (Empty, error) {
	if request.Password != nil || request.ImapHost != nil || request.ImapPort != nil || request.SmtpHost != nil || request.SmtpPort != nil || request.ImapSecurity != nil || request.SmtpSecurity != nil || request.AcceptInvalidCerts != nil || request.ProxyHost != nil || request.ProxyPort != nil {
		return Empty{}, errors.New("Connection and proxy settings are not supported yet")
	}
	if !validAccountText(request.DisplayName, 256) || (request.AccountLabel != nil && !validAccountText(*request.AccountLabel, 120)) {
		return Empty{}, errors.New("Invalid sender name or account label")
	}
	if request.AccountColor != nil {
		color := *request.AccountColor
		if len(color) != 7 || color[0] != '#' || strings.ContainsFunc(color[1:], func(r rune) bool { return !strings.ContainsRune("0123456789abcdefABCDEF", r) }) {
			return Empty{}, errors.New("Account color must be a six-digit hex color")
		}
	}
	if request.Signature != nil && !validSignature(*request.Signature) {
		return Empty{}, errors.New("Signature must be valid text of at most 64 KiB")
	}
	r := s.Runtime
	r.composeMu.Lock()
	defer r.composeMu.Unlock()
	account, err := r.DB.Account(ctx, request.AccountID)
	if err != nil || account.Provider != "gmail" || !strings.EqualFold(account.Email, request.Email) {
		return Empty{}, errors.New("An existing Gmail account with the same mailbox address is required")
	}
	settings := store.AccountSettings{AccountID: account.ID, Email: request.Email, DisplayName: strings.TrimSpace(request.DisplayName), Color: request.AccountColor, Label: request.AccountLabel}
	if request.Signature != nil {
		if err := r.verifyStoredSignature(ctx, account.ID); err != nil {
			return Empty{}, err
		}
		cipher, err := r.composeCrypto(ctx)
		if err != nil {
			return Empty{}, errors.New("Cannot unlock encrypted signature; account settings were not saved")
		}
		settings.Signature, err = cipher.Encrypt(signaturePurpose, account.ID, []byte(*request.Signature))
		if err != nil {
			return Empty{}, errors.New("Cannot encrypt signature; account settings were not saved")
		}
	}
	if err := r.DB.UpdateGmailSettings(ctx, settings); err != nil {
		return Empty{}, errors.New("Cannot save account settings; previous settings are preserved")
	}
	if r.OnAccountChanged != nil {
		r.OnAccountChanged(AccountRequest{AccountID: account.ID})
	}
	return Empty{}, nil
}

func (s *AccountService) Signature(ctx context.Context, request AccountRequest) (string, error) {
	r := s.Runtime
	r.composeMu.Lock()
	defer r.composeMu.Unlock()
	data, err := r.DB.EmailSignature(ctx, request.AccountID)
	if err != nil {
		return "", errors.New("Cannot read account signature")
	}
	if data == nil {
		return "", nil
	}
	return r.decryptSignature(ctx, request.AccountID, data)
}

// Caller holds composeMu. A valid-length key is not proof that it belongs to
// this profile: authenticate existing ciphertext before any replacement write.
func (r *Runtime) verifyStoredSignature(ctx context.Context, account string) error {
	data, err := r.DB.EmailSignature(ctx, account)
	if err != nil {
		return errors.New("Cannot read account signature; saved data is preserved")
	}
	if data != nil {
		_, err = r.decryptSignature(ctx, account, data)
	}
	return err
}

func (r *Runtime) decryptSignature(ctx context.Context, account string, data []byte) (string, error) {
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return "", errors.New("Cannot unlock encrypted signature; saved data is preserved")
	}
	plain, err := cipher.Decrypt(signaturePurpose, account, data)
	if err != nil || !validSignature(string(plain)) {
		return "", errors.New("Cannot decrypt signature; saved data is preserved")
	}
	return string(plain), nil
}
func (s *AccountService) SetSignature(ctx context.Context, request SignatureRequest) (Empty, error) {
	_, err := s.saveSignature(ctx, request, false)
	return Empty{}, err
}
func (s *AccountService) MigrateSignature(ctx context.Context, request SignatureRequest) (string, error) {
	return s.saveSignature(ctx, request, true)
}
func (s *AccountService) saveSignature(ctx context.Context, request SignatureRequest, onlyAbsent bool) (string, error) {
	if !validSignature(request.Signature) {
		return "", errors.New("Signature must be valid text of at most 64 KiB")
	}
	r := s.Runtime
	r.composeMu.Lock()
	defer r.composeMu.Unlock()
	if err := r.verifyStoredSignature(ctx, request.AccountID); err != nil {
		return "", err
	}
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return "", errors.New("Cannot unlock encrypted signature; saved data is preserved")
	}
	data, err := cipher.Encrypt(signaturePurpose, request.AccountID, []byte(request.Signature))
	if err != nil {
		return "", errors.New("Cannot encrypt signature")
	}
	stored, err := r.DB.SaveEmailSignature(ctx, request.AccountID, data, onlyAbsent)
	if err != nil {
		return "", errors.New("Cannot save signature; previous signature is preserved")
	}
	return r.decryptSignature(ctx, request.AccountID, stored)
}
