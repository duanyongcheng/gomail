package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const testMIME = "From: Sender <sender@example.test>\r\nTo: Receiver <receiver@example.test>\r\nBcc: Hidden <hidden@example.test>\r\nMessage-ID: <frozen@example.test>\r\nDate: Mon, 02 Nov 2026 10:00:00 +0000\r\nSubject: fixture\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nprivate fixture body"

func fixtureProvider(t *testing.T, handler http.HandlerFunc) (*Provider, string) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	api := NewAPI(server.Client(), func(context.Context) (string, error) { return "synthetic-bearer", nil })
	api.base = server.URL + "/v1.0/me"
	path := filepath.Join(t.TempDir(), "profile.sqlite")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.SaveAccount(t.Context(), domain.Account{ID: "graph-account", Email: "sender@example.test", Provider: "outlook"}, []byte("synthetic-encrypted-envelope")); err != nil {
		t.Fatal(err)
	}
	return New(db, api, "graph-account", "sender@example.test"), path
}

func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func foldersFixture(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/v1.0/me/mailFolders" {
		jsonResponse(w, map[string]any{"value": []any{map[string]any{"id": "inbox-id", "displayName": "Inbox", "childFolderCount": 0}, map[string]any{"id": "other-id", "displayName": "Other", "childFolderCount": 0}}})
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/v1.0/me/mailFolders/") && !strings.Contains(r.URL.Path, "/messages") {
		alias := strings.TrimPrefix(r.URL.Path, "/v1.0/me/mailFolders/")
		if alias == "inbox" {
			jsonResponse(w, map[string]string{"id": "inbox-id", "displayName": "Inbox"})
		} else {
			w.WriteHeader(404)
		}
		return true
	}
	return false
}

func TestGraphSyncReopenPartialHydrationAndMembershipTombstone(t *testing.T) {
	var phase atomic.Int32
	p, path := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Prefer") != `IdType="ImmutableId"` {
			t.Error("missing immutable IDs")
		}
		if foldersFixture(w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/$value") {
			fmt.Fprint(w, testMIME)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/messages/delta") {
			folder := strings.Split(r.URL.Path, "/")[4]
			var changes []any
			if folder == "inbox-id" {
				if phase.Load() == 0 {
					changes = []any{map[string]any{"id": "message-1", "isRead": true, "flag": map[string]string{"flagStatus": "flagged"}}}
				} else {
					changes = []any{map[string]any{"id": "message-1", "@removed": map[string]string{"reason": "deleted"}}}
				}
			}
			if folder == "other-id" && phase.Load() > 0 {
				changes = []any{map[string]any{"id": "message-1", "isRead": true}}
			}
			link := "https://" + r.Host + r.URL.Path + "?$deltatoken=boundary"
			jsonResponse(w, map[string]any{"value": changes, "@odata.deltaLink": link})
			return
		}
		t.Errorf("unexpected request %s", r.URL.Path)
		w.WriteHeader(404)
	})
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	id := store.ScopedID(p.AccountID, "message-1")
	m, err := p.DB.Message(t.Context(), id)
	if err != nil || !m.IsStarred || !m.IsRead || !strings.Contains(m.BodyText, "private fixture") {
		t.Fatalf("message not hydrated: %+v %v", m, err)
	}
	if err := p.DB.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p.DB = db
	phase.Store(1)
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	m, err = db.Message(t.Context(), id)
	if err != nil || !m.IsStarred {
		t.Fatalf("partial hydration lost flags or tombstone deleted global message: %+v %v", m, err)
	}
	inbox, err := db.Messages(t.Context(), p.folderID("inbox-id"), p.AccountID, false, 50, 0)
	if err != nil || len(inbox) != 0 {
		t.Fatalf("inbox membership survived tombstone: %v %v", inbox, err)
	}
	other, err := db.Messages(t.Context(), p.folderID("other-id"), p.AccountID, false, 50, 0)
	if err != nil || len(other) != 1 {
		t.Fatalf("other folder membership lost: %v %v", other, err)
	}
}

func TestGraphSendBarrierAcceptedAndReadOnlySentCheck(t *testing.T) {
	var submits, reads atomic.Int32
	p, _ := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/me":
			jsonResponse(w, map[string]string{"id": "stable-subject", "mail": "sender@example.test"})
		case "/v1.0/me/sendMail":
			submits.Add(1)
			w.WriteHeader(202)
		case "/v1.0/me/mailFolders/sentitems/messages":
			reads.Add(1)
			jsonResponse(w, map[string]any{"value": []any{map[string]string{"id": "sent-1", "internetMessageId": "frozen@example.test"}}})
		case "/v1.0/me/messages/sent-1/$value":
			fmt.Fprint(w, testMIME)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	mail := domain.OutgoingMail{Email: p.Email, Raw: []byte(testMIME)}
	result := p.Send(t.Context(), mail, func() error { return errors.New("synthetic persistence fault") })
	if result.Accepted || result.Unknown || submits.Load() != 0 {
		t.Fatalf("barrier submitted mail: %+v", result)
	}
	result = p.Send(t.Context(), mail, func() error { return nil })
	if !result.Accepted || result.RemoteID != "" || result.Unknown || submits.Load() != 1 {
		t.Fatalf("202 semantics %+v", result)
	}
	id, err := p.CheckSent(t.Context(), p.Email, "frozen@example.test")
	if err != nil || id != "sent-1" || submits.Load() != 1 || reads.Load() != 1 {
		t.Fatalf("read-only check %q %v", id, err)
	}
}

func TestGraphHydrationFailurePreservesCheckpoint(t *testing.T) {
	p, _ := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if foldersFixture(w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/messages/delta") {
			jsonResponse(w, map[string]any{"value": []any{map[string]string{"id": "bad-mime"}}, "@odata.deltaLink": "https://" + r.Host + r.URL.Path + "?$deltatoken=bad"})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/$value") {
			fmt.Fprint(w, "invalid MIME")
			return
		}
		w.WriteHeader(404)
	})
	if _, err := p.Sync(t.Context()); err == nil {
		t.Fatal("invalid MIME accepted")
	}
	if _, err := p.DB.ProviderCheckpoint(t.Context(), p.AccountID, "graph", "inbox-id"); err == nil {
		t.Fatal("checkpoint advanced past failed hydration")
	}
}

func TestGraphExpiredCursorReconcilesBoundedMemberships(t *testing.T) {
	var expired atomic.Bool
	p, _ := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if foldersFixture(w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/$value") {
			fmt.Fprint(w, testMIME)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/messages/delta") {
			if r.URL.Query().Get("$deltatoken") == "expired" {
				w.WriteHeader(410)
				expired.Store(true)
				return
			}
			jsonResponse(w, map[string]any{"value": []any{}, "@odata.deltaLink": "https://" + r.Host + r.URL.Path + "?$deltatoken=fresh"})
			return
		}
		w.WriteHeader(404)
	})
	ctx := t.Context()
	f := domain.Folder{ID: p.folderID("inbox-id"), AccountID: p.AccountID, RemoteID: "inbox-id", Name: "Inbox", FolderType: "folder"}
	m := domain.Message{ID: store.ScopedID(p.AccountID, "stale"), AccountID: p.AccountID, RemoteID: "stale", Subject: "cached before reset", To: []domain.Address{}, CC: []domain.Address{}, BCC: []domain.Address{}}
	if err := p.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: p.AccountID, Folders: []domain.Folder{f}, Messages: []store.CachedMessage{{Message: m, FolderIDs: []string{f.ID}}}, ProviderMappings: []store.ProviderMapping{{Provider: "graph", Scope: "inbox-id", RemoteID: "stale", MessageID: m.ID}}, ProviderCheckpoints: []store.ProviderCheckpoint{state("inbox-id", checkpoint{Link: p.API.base + "/mailFolders/inbox-id/messages/delta?$deltatoken=expired", Complete: true})}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if !expired.Load() {
		t.Fatal("fixture did not expire cursor")
	}
	rows, err := p.DB.Messages(ctx, f.ID, p.AccountID, false, 50, 0)
	if err != nil || len(rows) != 0 {
		t.Fatalf("stale membership retained: %v %v", rows, err)
	}
	if _, err := p.DB.Message(ctx, m.ID); err != nil {
		t.Fatal("reset destroyed readable cached message", err)
	}
	var cp checkpoint
	if err := p.load(ctx, "inbox-id", &cp); err != nil || cp.Reset || cp.Reconcile || !cp.Complete {
		t.Fatalf("reset did not settle: %+v %v", cp, err)
	}
}

func TestGraphFolderBarrierProtectsSystemAndPreventsWrite(t *testing.T) {
	var writes atomic.Int32
	p, _ := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes.Add(1)
			w.WriteHeader(500)
			return
		}
		if foldersFixture(w, r) {
			return
		}
		w.WriteHeader(404)
	})
	barriers := 0
	if _, err := p.ChangeFolder(t.Context(), "rename", "inbox-id", "New inbox", func() error { barriers++; return nil }); err == nil || barriers != 0 || writes.Load() != 0 {
		t.Fatal("system folder change was allowed")
	}
	if _, err := p.ChangeFolder(t.Context(), "create", "", "New", func() error { barriers++; return errors.New("disk fault") }); err == nil || writes.Load() != 0 || barriers != 1 {
		t.Fatal("failed barrier issued folder request")
	}
}

func TestReplayHTTPAuthorizationFailuresAreResourceStates(t *testing.T) {
	for _, operation := range []string{"update_flags", "move_to_folder"} {
		for _, status := range []int{401, 403, 500} {
			t.Run(fmt.Sprintf("%s/%d", operation, status), func(t *testing.T) {
				var calls atomic.Int32
				p, _ := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.WriteHeader(status)
				})
				id := store.ScopedID(p.AccountID, "message")
				if err := p.DB.CommitSnapshot(t.Context(), store.Snapshot{AccountID: p.AccountID, Messages: []store.CachedMessage{{Message: domain.Message{ID: id, AccountID: p.AccountID, RemoteID: "message"}}}}); err != nil {
					t.Fatal(err)
				}
				unknown, err := p.Replay(t.Context(), operation, json.RawMessage(`{"remote_id":"message","is_read":true,"target_remote_id":"destination"}`))
				if err == nil || calls.Load() != 1 {
					t.Fatal("write not exercised", err, calls.Load())
				}
				var access *domain.AccessError
				if status == 500 {
					if !unknown || errors.As(err, &access) {
						t.Fatal("ambiguous result lost quarantine", unknown, err)
					}
					return
				}
				want := domain.ConnectionReauthRequired
				if status == 403 {
					want = domain.ConnectionPermissionRequired
				}
				if unknown || !errors.As(err, &access) || access.State != want {
					t.Fatal("authorization did not reach queue resource policy", unknown, err)
				}
			})
		}
	}
}
