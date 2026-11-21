package app

import (
	"context"
	"errors"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/privacy"
)

type SenderRequest struct {
	AccountID string `json:"accountId"`
	Email     string `json:"email"`
}
type TrustRequest struct {
	AccountID string `json:"accountId"`
	Email     string `json:"email"`
	TrustType string `json:"trustType"`
}
type TrustService struct{ Runtime *Runtime }

func (s *TrustService) account(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("trust account required")
	}
	if _, err := s.Runtime.DB.Account(ctx, id); err != nil {
		return errors.New("trust account unavailable")
	}
	return nil
}
func (s *TrustService) List(ctx context.Context, request AccountRequest) ([]domain.TrustedSender, error) {
	if err := s.account(ctx, request.AccountID); err != nil {
		return nil, err
	}
	rows, err := s.Runtime.DB.TrustedSenders(ctx, request.AccountID)
	if err != nil {
		return nil, errors.New("cannot load trusted senders")
	}
	return rows, nil
}
func (s *TrustService) Set(ctx context.Context, request TrustRequest) (Empty, error) {
	if err := s.account(ctx, request.AccountID); err != nil {
		return Empty{}, err
	}
	if err := s.Runtime.DB.TrustSender(ctx, request.AccountID, request.Email, request.TrustType); err != nil {
		return Empty{}, errors.New("cannot save trust; check address and trust type")
	}
	s.changed(request.AccountID, request.Email)
	return Empty{}, nil
}
func (s *TrustService) Remove(ctx context.Context, request SenderRequest) (Empty, error) {
	if err := s.account(ctx, request.AccountID); err != nil {
		return Empty{}, err
	}
	if err := s.Runtime.DB.RemoveTrustedSender(ctx, request.AccountID, request.Email); err != nil {
		return Empty{}, errors.New("cannot remove trusted sender")
	}
	s.changed(request.AccountID, request.Email)
	return Empty{}, nil
}
func (s *TrustService) IsTrusted(ctx context.Context, request SenderRequest) (bool, error) {
	if err := s.account(ctx, request.AccountID); err != nil {
		return false, err
	}
	level, err := s.Runtime.DB.SenderTrust(ctx, request.AccountID, request.Email)
	if err != nil {
		return false, errors.New("cannot read sender trust")
	}
	return level != "", nil
}
func (s *TrustService) changed(account, email string) {
	if s.Runtime.OnTrustChanged != nil {
		normalized, _ := domain.NormalizeTrustedEmail(email)
		s.Runtime.OnTrustChanged(SenderRequest{AccountID: account, Email: normalized})
	}
}

// effectivePrivacyMode is the authorization boundary. A frontend TrustedSender
// object is a request, never proof of persisted trust or sender authenticity.
func (s *MessageService) effectivePrivacyMode(ctx context.Context, m domain.Message, request privacy.Mode) (privacy.Mode, error) {
	if request == privacy.Off {
		return privacy.Off, nil
	}
	if claimed, ok := request.TrustedEmail(); ok {
		actual, err := domain.NormalizeTrustedEmail(m.FromAddress)
		wanted, wantErr := domain.NormalizeTrustedEmail(claimed)
		if err != nil || wantErr != nil || wanted != actual {
			return privacy.Strict, nil
		}
	}
	level, err := s.Runtime.DB.SenderTrust(ctx, m.AccountID, m.FromAddress)
	if err != nil {
		return privacy.Strict, errors.New("cannot verify sender trust; HTML was not rendered")
	}
	switch level {
	case "images":
		return privacy.LoadOnce, nil
	case "all":
		return privacy.Mode("trusted:" + m.FromAddress), nil
	}
	if _, ok := request.TrustedEmail(); ok {
		return privacy.Strict, nil
	}
	return request, nil
}
