package gmail

import (
	"context"
	"errors"
	"strings"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/googleapi"
	"github.com/duanyongcheng/pebble-go/internal/store"
)

func labelFolders(account string, labels []googleapi.Label) []domain.Folder {
	folders := make([]domain.Folder, 0, len(labels))
	roles := map[string]string{"INBOX": "inbox", "SENT": "sent", "DRAFT": "drafts", "TRASH": "trash", "SPAM": "spam"}
	for _, label := range labels {
		var role *string
		if value, ok := roles[label.ID]; ok {
			role = &value
		}
		folders = append(folders, domain.Folder{ID: store.ScopedID(account, "label:"+label.ID), AccountID: account, RemoteID: label.ID, Name: label.Name, FolderType: "label", Role: role, IsSystem: label.Type == "system"})
	}
	return folders
}
func (p *Provider) labelSession(ctx context.Context) (*googleapi.API, error) {
	account, err := p.DB.Account(ctx, p.AccountID)
	if err != nil || account.Provider != "gmail" {
		return nil, errors.New("Gmail account unavailable")
	}
	return p.sendSession(ctx, account.Email)
}
func (p *Provider) refreshLabels(ctx context.Context, session *googleapi.API) ([]googleapi.Label, error) {
	labels, err := session.Labels(ctx)
	if err != nil {
		return nil, errors.New("cannot read Gmail labels; cached folders preserved")
	}
	if err := p.DB.ReconcileGmailLabels(ctx, p.AccountID, labelFolders(p.AccountID, labels)); err != nil {
		return nil, errors.New("cannot update cached Gmail labels; refresh again")
	}
	return labels, nil
}
func (p *Provider) RefreshLabels(ctx context.Context) error {
	p.gate.Lock()
	defer p.gate.Unlock()
	session, err := p.labelSession(ctx)
	if err != nil {
		return err
	}
	_, err = p.refreshLabels(ctx, session)
	return err
}
func (p *Provider) ChangeLabel(ctx context.Context, change domain.LabelChange) error {
	if change.Action != "create" && change.Action != "rename" && change.Action != "delete" {
		return errors.New("invalid Gmail label action")
	}
	var err error
	if change.Action != "delete" {
		change.Name, err = domain.ValidateGmailLabelName(change.Name)
		if err != nil {
			return err
		}
	}
	p.gate.Lock()
	defer p.gate.Unlock()
	if pending, err := p.DB.GmailLabelChange(ctx, p.AccountID); err != nil {
		return errors.New("cannot inspect pending label changes")
	} else if pending != nil {
		return errors.New("previous label change needs reconciliation; refresh or resolve it first")
	}
	remoteID := ""
	if change.Action != "create" {
		owner, remote, err := p.DB.Folder(ctx, change.FolderID)
		if err != nil || owner != p.AccountID || domain.GmailSystemLabel(remote) {
			return errors.New("only this account's custom labels can be changed")
		}
		remoteID = remote
	}
	session, err := p.labelSession(ctx)
	if err != nil {
		return err
	}
	labels, err := p.refreshLabels(ctx, session)
	if err != nil {
		return err
	}
	found := change.Action == "create"
	for _, label := range labels {
		if label.ID == remoteID {
			if label.Type != "user" {
				return errors.New("system labels cannot be changed")
			}
			found = true
			if change.Action == "delete" {
				change.Name = label.Name
			}
			if change.Action == "rename" && label.Name == change.Name {
				return nil
			}
		}
		if change.Action != "delete" && label.ID != remoteID && strings.EqualFold(label.Name, change.Name) {
			return errors.New("a Gmail label with this name already exists")
		}
	}
	if !found {
		return errors.New("label is no longer available; cached list was refreshed")
	}
	intent := store.GmailLabelChange{Action: change.Action, RemoteID: remoteID, Name: change.Name}
	id, err := p.DB.BeginGmailLabelChange(ctx, p.AccountID, intent)
	if err != nil {
		return errors.New("label change blocked by unresolved management or pending mail operations")
	}
	result, writeErr := session.ChangeLabel(ctx, change.Action, remoteID, change.Name)
	if writeErr != nil {
		if !result.Uncertain {
			if err := p.DB.ClearGmailLabelChange(ctx, p.AccountID, id); err != nil {
				return errors.New("label result could not be saved; refresh before retrying")
			}
		}
		return writeErr
	}
	updated := make([]googleapi.Label, 0, len(labels)+1)
	for _, label := range labels {
		if label.ID == remoteID {
			if change.Action == "delete" {
				continue
			}
			label = result.Label
		}
		updated = append(updated, label)
	}
	if change.Action == "create" {
		updated = append(updated, result.Label)
	}
	if err := googleapi.ValidateLabels(updated); err != nil {
		return errors.New("label result needs refresh before continuing")
	}
	if err := p.DB.ReconcileGmailLabels(ctx, p.AccountID, labelFolders(p.AccountID, updated)); err != nil {
		return errors.New("Gmail accepted the label change but local persistence failed; refresh labels")
	}
	return nil
}
