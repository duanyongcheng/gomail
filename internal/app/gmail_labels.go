package app

import (
	"context"
	"errors"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/store"
)

type GmailLabelsView struct {
	Folders []domain.Folder         `json:"folders"`
	Pending *store.GmailLabelChange `json:"pending"`
}
type GmailLabelRequest struct {
	AccountID string `json:"accountId"`
	Action    string `json:"action"`
	FolderID  string `json:"folderId"`
	Name      string `json:"name"`
	PendingID string `json:"pendingId"`
}
type GmailLabelService struct{ Runtime *Runtime }

func (s *GmailLabelService) List(ctx context.Context, request AccountRequest) (GmailLabelsView, error) {
	account, err := s.Runtime.DB.Account(ctx, request.AccountID)
	if err != nil || account.Provider != "gmail" {
		return GmailLabelsView{}, errors.New("Gmail account required")
	}
	folders, err := s.Runtime.DB.Folders(ctx, account.ID)
	if err != nil {
		return GmailLabelsView{}, errors.New("cannot read cached Gmail labels")
	}
	pending, err := s.Runtime.DB.GmailLabelChange(ctx, account.ID)
	if err != nil {
		return GmailLabelsView{}, errors.New("cannot read pending label changes")
	}
	return GmailLabelsView{Folders: folders, Pending: pending}, nil
}
func (s *GmailLabelService) Change(ctx context.Context, request GmailLabelRequest) (GmailLabelsView, error) {
	r := s.Runtime
	// Serialize management/dismiss so a user cannot clear an intent while its
	// HTTP request is still running, then accidentally create another one.
	r.labelManagementMu.Lock()
	defer r.labelManagementMu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context, cancel)
	defer stop()
	account, err := r.DB.Account(ctx, request.AccountID)
	if err != nil || account.Provider != "gmail" {
		return GmailLabelsView{}, errors.New("Gmail account required")
	}
	defer func() {
		if r.OnFoldersChanged != nil {
			r.OnFoldersChanged(AccountRequest{AccountID: request.AccountID})
		}
	}()
	if request.Action == "dismiss" {
		if request.PendingID == "" {
			return GmailLabelsView{}, errors.New("pending label change id required")
		}
		if err := r.DB.ClearGmailLabelChange(ctx, request.AccountID, request.PendingID); err != nil {
			return GmailLabelsView{}, errors.New("cannot dismiss label change")
		}
	} else {
		p, err := r.provider(request.AccountID)
		if err != nil {
			return GmailLabelsView{}, err
		}
		labels, ok := p.(domain.LabelProvider)
		if !ok {
			return GmailLabelsView{}, errors.New("provider does not support label management")
		}
		if request.Action == "refresh" {
			err = labels.RefreshLabels(ctx)
		} else {
			err = labels.ChangeLabel(ctx, domain.LabelChange{Action: request.Action, FolderID: request.FolderID, Name: request.Name})
		}
		if err != nil {
			return GmailLabelsView{}, err
		}
	}
	return s.List(ctx, AccountRequest{AccountID: request.AccountID})
}
