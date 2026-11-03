package app

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/store"
)

type FolderManagementView struct {
	Folders []domain.Folder         `json:"folders"`
	Pending *store.GmailLabelChange `json:"pending"`
}
type FolderManagementRequest struct {
	AccountID string `json:"accountId"`
	Action    string `json:"action"`
	FolderID  string `json:"folderId"`
	Name      string `json:"name"`
	PendingID string `json:"pendingId"`
}

func (s *FolderService) Management(ctx context.Context, request AccountRequest) (FolderManagementView, error) {
	a, err := s.Runtime.DB.Account(ctx, request.AccountID)
	if err != nil || (a.Provider != "imap" && a.Provider != "outlook") {
		return FolderManagementView{}, errors.New("provider does not support folder management")
	}
	folders, err := s.Runtime.DB.Folders(ctx, a.ID)
	if err != nil {
		return FolderManagementView{}, err
	}
	pending, err := s.Runtime.DB.GmailLabelChange(ctx, a.ID)
	return FolderManagementView{Folders: folders, Pending: pending}, err
}

func (s *FolderService) Manage(ctx context.Context, request FolderManagementRequest) (FolderManagementView, error) {
	r := s.Runtime
	r.labelManagementMu.Lock()
	defer r.labelManagementMu.Unlock()
	view, err := s.Management(ctx, AccountRequest{AccountID: request.AccountID})
	if err != nil {
		return view, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context, cancel)
	defer stop()
	defer func() {
		if r.OnFoldersChanged != nil {
			r.OnFoldersChanged(AccountRequest{AccountID: request.AccountID})
		}
	}()
	if request.Action == "dismiss" {
		if request.PendingID == "" {
			return view, errors.New("pending action identity required")
		}
		err = r.DB.ClearGmailLabelChange(ctx, request.AccountID, request.PendingID)
		if err != nil {
			return view, err
		}
		return s.Management(ctx, AccountRequest{AccountID: request.AccountID})
	}
	p, err := r.provider(request.AccountID)
	if err != nil {
		return view, err
	}
	folders, ok := p.(domain.FolderProvider)
	if !ok {
		return view, errors.New("folder listing unavailable")
	}
	refresh := func() error {
		if refresher, ok := p.(interface{ RefreshFolders(context.Context) error }); ok {
			if err := refresher.RefreshFolders(ctx); err != nil {
				return err
			}
		}
		list, err := folders.Folders(ctx)
		if err != nil {
			return err
		}
		return r.DB.ReconcileProtocolFolders(ctx, request.AccountID, list)
	}
	if request.Action == "refresh" {
		if err := refresh(); err != nil {
			return view, err
		}
		return s.Management(ctx, AccountRequest{AccountID: request.AccountID})
	}
	if view.Pending != nil {
		return view, errors.New("folder action outcome is unresolved; refresh or dismiss it first")
	}
	if request.Action != "create" && request.Action != "rename" && request.Action != "delete" {
		return view, errors.New("invalid folder action")
	}
	name := strings.TrimSpace(request.Name)
	if request.Action != "delete" {
		if name == "" || len(name) > 225 || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) {
			return view, errors.New("invalid folder name")
		}
		for _, f := range view.Folders {
			if strings.EqualFold(f.Name, name) {
				return view, errors.New("folder name already exists")
			}
		}
	}
	remote := ""
	if request.Action != "create" {
		for _, f := range view.Folders {
			if f.ID == request.FolderID && f.AccountID == request.AccountID && !f.IsSystem && f.Role == nil {
				remote = f.RemoteID
			}
		}
		if remote == "" {
			return view, errors.New("custom account-owned folder required")
		}
	}
	writer, ok := p.(interface {
		ChangeFolder(context.Context, string, string, string, func() error) (bool, error)
	})
	if !ok {
		return view, errors.New("folder changes unavailable")
	}
	started := false
	_, err = writer.ChangeFolder(ctx, request.Action, remote, name, func() error {
		if started {
			return errors.New("folder barrier already crossed")
		}
		_, err := r.DB.BeginGmailLabelChange(ctx, request.AccountID, store.GmailLabelChange{Action: request.Action, RemoteID: remote, Name: name})
		started = err == nil
		return err
	})
	if err != nil {
		return view, errors.New("folder action failed; refresh to check any unresolved outcome")
	}
	if !started {
		return view, errors.New("provider omitted durable folder barrier")
	}
	if err = refresh(); err != nil {
		return view, errors.New("folder action submitted; refresh to reconcile its outcome")
	}
	return s.Management(ctx, AccountRequest{AccountID: request.AccountID})
}
