package app

import (
	"context"
	"errors"
	"time"

	"github.com/bary/pebble-go/internal/domain"
)

type IMAPSyncFolders struct {
	Folders           []domain.Folder `json:"folders"`
	SelectedRemoteIDs []string        `json:"selected_remote_ids"`
}
type IMAPSyncFoldersRequest struct {
	AccountID         string   `json:"accountId"`
	SelectedRemoteIDs []string `json:"selectedRemoteIds"`
}

func (s *AccountService) SyncFolders(ctx context.Context, request AccountRequest) (IMAPSyncFolders, error) {
	a, err := s.Runtime.DB.Account(ctx, request.AccountID)
	if err != nil || a.Provider != "imap" {
		return IMAPSyncFolders{}, errors.New("IMAP account required")
	}
	settings, err := s.ProtocolSettings(ctx, request)
	if err != nil {
		return IMAPSyncFolders{}, err
	}
	folders, err := s.Runtime.DB.Folders(ctx, a.ID)
	return IMAPSyncFolders{Folders: folders, SelectedRemoteIDs: settings.SelectedFolders}, err
}
func (s *AccountService) SetSyncFolders(ctx context.Context, request IMAPSyncFoldersRequest) (IMAPSyncFolders, error) {
	a, err := s.Runtime.DB.Account(ctx, request.AccountID)
	if err != nil || a.Provider != "imap" {
		return IMAPSyncFolders{}, errors.New("IMAP account required")
	}
	r := s.Runtime
	r.mu.RLock()
	c, encrypted, err := r.loadProtocol(ctx, a.ID)
	r.mu.RUnlock()
	if err != nil {
		return IMAPSyncFolders{}, err
	}
	c.SelectedFolders = request.SelectedRemoteIDs
	if _, err := s.SaveProtocol(ctx, SaveProtocolRequest{AccountID: a.ID, Revision: protocolRevision(encrypted), DisplayName: a.DisplayName, Config: c}); err != nil {
		return IMAPSyncFolders{}, err
	}
	return s.SyncFolders(ctx, AccountRequest{AccountID: a.ID})
}

type XOAuth2Status struct {
	IMAPUsesToken   bool    `json:"imap_uses_token"`
	SMTPUsesToken   bool    `json:"smtp_uses_token"`
	HasRefreshToken bool    `json:"has_refresh_token"`
	ExpiresAt       *int64  `json:"expires_at"`
	ExpiresInSecs   *int64  `json:"expires_in_secs"`
	Tenant          *string `json:"tenant"`
	ClientID        *string `json:"client_id"`
	HasClientSecret bool    `json:"has_client_secret"`
}

func (s *AccountService) XOAuth2Status(ctx context.Context, request AccountRequest) (XOAuth2Status, error) {
	r := s.Runtime
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, _, err := r.loadProtocol(ctx, request.AccountID)
	if err != nil {
		return XOAuth2Status{}, err
	}
	out := XOAuth2Status{IMAPUsesToken: c.IMAP.AuthMode == "xoauth2", SMTPUsesToken: c.SMTP.AuthMode == "xoauth2"}
	if c.Microsoft == nil {
		return out, nil
	}
	m := c.Microsoft
	out.Tenant, out.ClientID = &m.Tenant, &m.ClientID
	grant := m.Grants["exchange"]
	out.HasRefreshToken = grant.RefreshToken != ""
	if grant.Expiry > 0 {
		expiry, remaining := grant.Expiry, grant.Expiry-time.Now().Unix()
		out.ExpiresAt, out.ExpiresInSecs = &expiry, &remaining
	}
	return out, nil
}
