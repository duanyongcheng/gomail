package app

import (
	"testing"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/provider/gmail"
	"github.com/bary/pebble-go/internal/testgmail"
)

func TestGmailLabelServiceIsLocalUntilExplicitAction(t *testing.T) {
	compose, _ := composeFixture(t)
	r := compose.Runtime
	service := &GmailLabelService{Runtime: r}
	mock := testgmail.NewMock()
	defer mock.Close()
	mock.ProfileEmail = "a@example.invalid"
	r.InstallProvider("a", gmail.New(r.DB, mock.API(staticToken{"demo-access"}), "a"))
	if view, err := service.List(t.Context(), AccountRequest{AccountID: "a"}); err != nil || len(view.Folders) != 0 {
		t.Fatal(view, err)
	}
	mock.Lock()
	calls := len(mock.Requests)
	mock.Unlock()
	if calls != 0 {
		t.Fatal("local list contacted Gmail")
	}
	changed := 0
	r.OnFoldersChanged = func(request AccountRequest) {
		if request.AccountID != "a" {
			t.Error(request)
		}
		changed++
	}
	if _, err := service.Change(t.Context(), GmailLabelRequest{AccountID: "a", Action: "create", Name: "Created"}); err != nil {
		t.Fatal(err)
	}
	view, err := service.List(t.Context(), AccountRequest{AccountID: "a"})
	if err != nil || len(view.Folders) == 0 || view.Pending != nil || changed != 1 {
		t.Fatal(view, err, changed)
	}
	if err := r.DB.SaveAccount(t.Context(), domain.Account{ID: "other", Email: "other", Provider: "imap"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(t.Context(), AccountRequest{AccountID: "other"}); err == nil {
		t.Fatal("non-Gmail accepted")
	}
}
