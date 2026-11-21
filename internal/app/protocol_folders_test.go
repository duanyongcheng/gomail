package app

import (
	"context"
	"errors"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

type folderFixtureProvider struct {
	scheduledProvider
	folders []domain.Folder
	writes  int
	unknown bool
}

func (p *folderFixtureProvider) Folders(context.Context) ([]domain.Folder, error) {
	return p.folders, nil
}
func (p *folderFixtureProvider) ChangeFolder(ctx context.Context, action, remote, name string, barrier func() error) (bool, error) {
	if err := barrier(); err != nil {
		return false, err
	}
	p.writes++
	if p.unknown {
		return true, errors.New("lost receipt")
	}
	p.folders = append(p.folders, domain.Folder{ID: "new-folder", AccountID: "a", RemoteID: name, Name: name, FolderType: "folder"})
	return false, nil
}

func TestProtocolFolderBarrierPersistsUnknownAndPreventsReplay(t *testing.T) {
	compose, _ := composeFixture(t)
	r := compose.Runtime
	// Fixtures own this disposable DB; no production profile is opened.
	injectPendingSQL(t, r.DB, "UPDATE accounts SET provider='imap' WHERE id='a'")
	p := &folderFixtureProvider{unknown: true}
	r.InstallProvider("a", p)
	s := &FolderService{Runtime: r}
	req := FolderManagementRequest{AccountID: "a", Action: "create", Name: "Uncertain folder"}
	if _, err := s.Manage(t.Context(), req); err == nil {
		t.Fatal("unknown write claimed success")
	}
	if _, err := s.Manage(t.Context(), req); err == nil {
		t.Fatal("unresolved folder write replayed")
	}
	if p.writes != 1 {
		t.Fatal("replayed", p.writes)
	}
	view, err := s.Management(t.Context(), AccountRequest{AccountID: "a"})
	if err != nil || view.Pending == nil {
		t.Fatal(view, err)
	}
	// A complete read-only observation reconciles the retained barrier.
	p.folders = []domain.Folder{{ID: "observed", AccountID: "a", RemoteID: req.Name, Name: req.Name, FolderType: "folder"}}
	view, err = s.Manage(t.Context(), FolderManagementRequest{AccountID: "a", Action: "refresh"})
	if err != nil || view.Pending != nil || p.writes != 1 {
		t.Fatal(view, err, p.writes)
	}
}

func TestProtocolFolderBarrierFailurePreventsRemoteCall(t *testing.T) {
	compose, _ := composeFixture(t)
	r := compose.Runtime
	injectPendingSQL(t, r.DB, "UPDATE accounts SET provider='imap' WHERE id='a'")
	p := &folderFixtureProvider{}
	r.InstallProvider("a", p)
	injectPendingSQL(t, r.DB, `CREATE TRIGGER fail_folder_intent BEFORE INSERT ON gmail_label_changes BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := (&FolderService{Runtime: r}).Manage(t.Context(), FolderManagementRequest{AccountID: "a", Action: "create", Name: "No write"}); err == nil {
		t.Fatal("barrier error hidden")
	}
	if p.writes != 0 {
		t.Fatal("write escaped failed barrier")
	}
	pending, err := r.DB.GmailLabelChange(t.Context(), "a")
	if err != nil || pending != nil {
		t.Fatal(pending, err)
	}
}
