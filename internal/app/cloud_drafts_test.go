package app

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/duanyongcheng/gomail/internal/googleapi"
	"github.com/duanyongcheng/gomail/internal/mailmime"
	"github.com/duanyongcheng/gomail/internal/provider/gmail"
)

type draftServer struct {
	mu                    sync.Mutex
	writes                atomic.Int32
	gets                  int
	raw, message, email   string
	lose, reject, missing bool
	beforeWrite           func()
}

func cloudFixture(t *testing.T) (*ComposeService, *draftServer, string) {
	s, _ := composeFixture(t)
	fixture := &draftServer{email: "a@example.invalid"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		fixture.gets++
		if r.URL.Path == "/profile" {
			json.NewEncoder(w).Encode(map[string]string{"emailAddress": fixture.email, "historyId": "123"})
			return
		}
		if r.Method == "POST" || r.Method == "PUT" {
			fixture.writes.Add(1)
			if fixture.beforeWrite != nil {
				fixture.beforeWrite()
			}
			if fixture.reject {
				http.Error(w, "rejected", 400)
				return
			}
			var body struct {
				Message struct {
					Raw string `json:"raw"`
				} `json:"message"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			fixture.raw = body.Message.Raw
			fixture.message = fmt.Sprint("msg", fixture.writes.Load())
			if fixture.lose {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				conn.Close()
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "draft1", "message": map[string]string{"id": fixture.message}})
			return
		}
		if r.URL.Path == "/drafts" {
			json.NewEncoder(w).Encode(map[string]any{"drafts": []any{map[string]any{"id": "draft1", "message": map[string]string{"id": fixture.message}}}})
			return
		}
		if fixture.missing {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "draft1", "message": map[string]any{"id": fixture.message, "raw": fixture.raw, "labelIds": []string{"DRAFT"}}})
	}))
	t.Cleanup(server.Close)
	s.Runtime.InstallProvider("a", gmail.New(s.Runtime.DB, &googleapi.API{Base: server.URL, HTTP: server.Client(), Tokens: staticToken{"token"}}, "a"))
	id, err := s.Save(t.Context(), SaveDraftRequest{AccountID: "a", Subject: "private", BodyText: "body", BodyHTML: "<p>body</p>", EditorMode: "rich", BCC: []string{"hidden@example.invalid"}})
	if err != nil {
		t.Fatal(err)
	}
	return s, fixture, id
}
func TestCloudDraftCreateUpdateAndExternalConflict(t *testing.T) {
	s, f, id := cloudFixture(t)
	ctx := t.Context()
	request := CloudDraftRequest{AccountID: "a", DraftID: id, Action: "upload"}
	got, err := s.Cloud(ctx, request)
	if err != nil || got.State != "synced" || f.writes.Load() != 1 {
		t.Fatal(got, err, f.writes.Load())
	}
	f.mu.Lock()
	raw, _ := base64.RawURLEncoding.DecodeString(f.raw)
	f.mu.Unlock()
	parsed, err := mailmime.ParseWithLimit(raw, mailmime.MaxOutgoingBytes)
	if err != nil || len(parsed.BCC) != 1 || parsed.BCC[0].Address != "hidden@example.invalid" || parsed.From.Address != "a@example.invalid" {
		t.Fatal(parsed, err)
	}
	copy, err := s.Runtime.DB.GmailDraftCopy(ctx, "a", id)
	if err != nil || strings.Contains(string(copy.Data), "gomail.invalid") {
		t.Fatal(copy, err)
	}
	if _, err = s.Save(ctx, SaveDraftRequest{AccountID: "a", ExistingDraftID: id, Subject: "updated", BodyText: "new", EditorMode: "rich"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx, AccountRequest{AccountID: "a"})
	if err != nil || list[0].CloudState != "changed" {
		t.Fatal(list, err)
	}
	if _, err = s.Cloud(ctx, request); err != nil || f.writes.Load() != 2 {
		t.Fatal(err, f.writes.Load())
	}
	f.mu.Lock()
	f.message = "web-edited"
	f.mu.Unlock()
	if _, err = s.Cloud(ctx, request); err == nil || f.writes.Load() != 2 {
		t.Fatal("overwrote external edit", err, f.writes.Load())
	}
	list, err = s.List(ctx, AccountRequest{AccountID: "a"})
	if err != nil || list[0].CloudState != "conflict" {
		t.Fatal(list, err)
	}
}
func TestCloudDraftLostResponsePersistsAndReadOnlyReconciles(t *testing.T) {
	s, f, id := cloudFixture(t)
	ctx := t.Context()
	f.mu.Lock()
	f.lose = true
	f.mu.Unlock()
	request := CloudDraftRequest{AccountID: "a", DraftID: id, Action: "upload"}
	if _, err := s.Cloud(ctx, request); err == nil || f.writes.Load() != 1 {
		t.Fatal(err)
	}
	if _, err := s.Delete(ctx, DraftRequest{AccountID: "a", DraftID: id}); err == nil {
		t.Fatal("removed unconfirmed draft")
	}
	// Recreate runtime (no in-memory retry guard) and prove durable intent wins.
	next := New(ctx, s.Runtime.DB, s.Runtime.Keyring, nil)
	defer next.Scheduler.Close()
	p, _ := s.Runtime.provider("a")
	next.InstallProvider("a", p)
	s = &ComposeService{Runtime: next}
	if _, err := s.Cloud(ctx, request); err == nil || f.writes.Load() != 1 {
		t.Fatal("repeated unknown write", err)
	}
	request.Action = "check"
	got, err := s.Cloud(ctx, request)
	if err != nil || got.State != "synced" || f.writes.Load() != 1 {
		t.Fatal(got, err, f.writes.Load())
	}
	if _, err := s.Get(ctx, DraftRequest{AccountID: "a", DraftID: id}); err != nil {
		t.Fatal("lost local draft", err)
	}
}
func TestCloudDraftReceiptFailureNeverRepeatsWrite(t *testing.T) {
	s, f, id := cloudFixture(t)
	ctx := t.Context()
	db, err := sql.Open("sqlite", s.Runtime.DB.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER fail_receipt BEFORE UPDATE ON gmail_draft_copies WHEN NEW.state='synced' BEGIN SELECT RAISE(ABORT,'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	req := CloudDraftRequest{AccountID: "a", DraftID: id, Action: "upload"}
	if _, err := s.Cloud(ctx, req); err == nil {
		t.Fatal("receipt failure ignored")
	}
	if _, err := s.Cloud(ctx, req); err == nil || f.writes.Load() != 1 {
		t.Fatal("repeated accepted write", err)
	}
	if _, err := db.Exec("DROP TRIGGER fail_receipt"); err != nil {
		t.Fatal(err)
	}
	req.Action = "check"
	if _, err := s.Cloud(ctx, req); err != nil || f.writes.Load() != 1 {
		t.Fatal(err)
	}
}

func TestCloudDraftNetworkDoesNotBlockLocalAutosave(t *testing.T) {
	s, f, id := cloudFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	f.beforeWrite = func() { close(started); <-release }
	done := make(chan error, 1)
	go func() {
		_, err := s.Cloud(t.Context(), CloudDraftRequest{AccountID: "a", DraftID: id, Action: "upload"})
		done <- err
	}()
	<-started
	saved := make(chan error, 1)
	go func() {
		_, err := s.Save(t.Context(), SaveDraftRequest{AccountID: "a", ExistingDraftID: id, Subject: "new offline edit", EditorMode: "rich"})
		saved <- err
	}()
	select {
	case err := <-saved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("network blocked autosave")
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	items, err := s.List(t.Context(), AccountRequest{AccountID: "a"})
	if err != nil || items[0].CloudState != "changed" {
		t.Fatal(items, err)
	}
}

func TestCloudDraftAttachmentsAndWrongKey(t *testing.T) {
	s, f, id := cloudFixture(t)
	ctx := t.Context()
	path, err := s.Stage(ctx, StageComposeRequest{Filename: "private.txt", Bytes: []int{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, SaveDraftRequest{AccountID: "a", ExistingDraftID: id, Subject: "attachment", EditorMode: "rich", AttachmentPaths: []string{path}, QuotedReplyHTML: "<blockquote>quote</blockquote>"}); err != nil {
		t.Fatal(err)
	}
	req := CloudDraftRequest{AccountID: "a", DraftID: id, Action: "upload"}
	if _, err := s.Cloud(ctx, req); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	raw, _ := base64.RawURLEncoding.DecodeString(f.raw)
	f.mu.Unlock()
	m, err := mailmime.ParseWithLimit(raw, mailmime.MaxOutgoingBytes)
	if err != nil || len(m.Attachments) != 1 || !bytes.Equal(m.Attachments[0].Data, []byte{1, 2, 3}) || !strings.Contains(m.BodyHTML, "quote") {
		t.Fatal(m, err)
	}
	// Gmail-added headers do not invalidate a matching semantic snapshot.
	f.mu.Lock()
	f.raw = base64.RawURLEncoding.EncodeToString(append([]byte("Received: by fixture\r\n"), raw...))
	f.mu.Unlock()
	req.Action = "check"
	if _, err := s.Cloud(ctx, req); err != nil {
		t.Fatal(err)
	}
	next := New(ctx, s.Runtime.DB, &testKeyring{key: bytes.Repeat([]byte{8}, 32)}, nil)
	defer next.Scheduler.Close()
	p, _ := s.Runtime.provider("a")
	next.InstallProvider("a", p)
	if _, err := (&ComposeService{Runtime: next}).Cloud(ctx, req); err == nil || f.writes.Load() != 1 {
		t.Fatal("wrong key allowed write", err)
	}
}

func TestCloudDraftIsolationValidationAndRejection(t *testing.T) {
	for _, kind := range []string{"account", "mailbox", "recipient", "markdown", "rejection", "missing"} {
		t.Run(kind, func(t *testing.T) {
			s, f, id := cloudFixture(t)
			ctx := t.Context()
			request := CloudDraftRequest{AccountID: "a", DraftID: id, Action: "upload"}
			switch kind {
			case "account":
				request.AccountID = "b"
			case "mailbox":
				f.mu.Lock()
				f.email = "other@example.invalid"
				f.mu.Unlock()
			case "recipient", "markdown":
				d := SaveDraftRequest{AccountID: "a", ExistingDraftID: id, Subject: "saved", EditorMode: "rich"}
				if kind == "recipient" {
					d.To = []string{"partial"}
				} else {
					d.EditorMode = "markdown"
				}
				if _, err := s.Save(ctx, d); err != nil {
					t.Fatal(err)
				}
			case "rejection":
				f.mu.Lock()
				f.reject = true
				f.mu.Unlock()
			case "missing":
				if _, err := s.Cloud(ctx, request); err != nil {
					t.Fatal(err)
				}
				f.mu.Lock()
				f.missing = true
				f.mu.Unlock()
			}
			if _, err := s.Cloud(ctx, request); err == nil {
				t.Fatal("invalid operation succeeded")
			}
			if kind != "missing" && kind != "rejection" && f.writes.Load() != 0 {
				t.Fatal("unexpected network write")
			}
			if kind == "rejection" {
				f.mu.Lock()
				f.reject = false
				f.mu.Unlock()
				if _, err := s.Cloud(ctx, request); err != nil || f.writes.Load() != 2 {
					t.Fatal("explicit rejection not retryable", err)
				}
			}
		})
	}
}
