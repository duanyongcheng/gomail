package testgmail

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"

	"golang.org/x/oauth2"
)

// Mock is a local OAuth/Gmail fixture server, never Google's real service.
type Mock struct {
	Server            *httptest.Server
	mu                sync.Mutex
	Messages          map[string]Message
	HistoryID         string
	Expired           bool
	FailStatus        int
	PageLoop          bool
	Refreshes         int
	RejectRefresh     bool
	HistoryPages      bool
	FailMessage       string
	ExpectedChallenge string
	Requests          []string
}

func (m *Mock) Lock()   { m.mu.Lock() }
func (m *Mock) Unlock() { m.mu.Unlock() }

func NewMock() *Mock {
	m := &Mock{Messages: map[string]Message{}, HistoryID: "900719925474099312345"}
	m.Server = httptest.NewServer(http.HandlerFunc(m.serve))
	return m
}
func (m *Mock) Close() { m.Server.Close() }
func (m *Mock) OAuthConfig() *oauth2.Config {
	return &oauth2.Config{ClientID: "demo-client", ClientSecret: "demo-secret", Scopes: []string{ModifyScope}, Endpoint: oauth2.Endpoint{AuthURL: m.Server.URL + "/authorize", TokenURL: m.Server.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}}
}
func (m *Mock) API(tokens TokenSource) *API {
	return &API{Base: m.Server.URL + "/gmail/v1/users/me", HTTP: m.Server.Client(), Tokens: tokens}
}
func (m *Mock) Set(id string, raw []byte, labels []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Messages[id] = Message{ID: id, ThreadID: "thread-" + id, Labels: labels, Raw: base64.RawURLEncoding.EncodeToString(raw), Date: "1700000000000", HistoryID: m.HistoryID}
}
func (m *Mock) Delete(id string)        { m.mu.Lock(); defer m.mu.Unlock(); delete(m.Messages, id) }
func (m *Mock) ChangeHistory(id string) { m.mu.Lock(); m.HistoryID = id; m.mu.Unlock() }
func (m *Mock) serve(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Requests = append(m.Requests, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/authorize" {
		q := r.URL.Query()
		m.ExpectedChallenge = q.Get("code_challenge")
		target, _ := url.Parse(q.Get("redirect_uri"))
		query := target.Query()
		query.Set("code", "demo-code")
		query.Set("state", q.Get("state"))
		target.RawQuery = query.Encode()
		http.Redirect(w, r, target.String(), 302)
		return
	}
	if r.URL.Path == "/token" {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "authorization_code" {
			if oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != m.ExpectedChallenge {
				http.Error(w, "invalid PKCE", 400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "demo-access", "refresh_token": "demo-refresh", "token_type": "Bearer", "expires_in": 3600})
		} else {
			m.Refreshes++
			if m.RejectRefresh {
				http.Error(w, "invalid_grant", 400)
				return
			}
			if r.Form.Get("refresh_token") != "demo-refresh" {
				http.Error(w, "invalid_grant", 400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "refreshed-access", "token_type": "Bearer", "expires_in": 3600})
		}
		return
	}
	if r.Header.Get("Authorization") != "Bearer demo-access" && r.Header.Get("Authorization") != "Bearer refreshed-access" {
		w.WriteHeader(401)
		return
	}
	if m.FailStatus != 0 {
		status := m.FailStatus
		m.FailStatus = 0
		w.WriteHeader(status)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me")
	switch path {
	case "/profile":
		_ = json.NewEncoder(w).Encode(Profile{Email: "demo@gmail.example.invalid", HistoryID: m.HistoryID})
	case "/labels":
		_ = json.NewEncoder(w).Encode(map[string]any{"labels": []Label{{ID: "INBOX", Name: "INBOX", Type: "system"}, {ID: "UNREAD", Name: "UNREAD", Type: "system"}, {ID: "STARRED", Name: "STARRED", Type: "system"}, {ID: "Label_work", Name: "工作", Type: "user"}}})
	case "/messages":
		ids := make([]string, 0, len(m.Messages))
		for id := range m.Messages {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		page := r.URL.Query().Get("pageToken")
		start := 0
		if page != "" {
			start = 1
		}
		end := start + 1
		if end > len(ids) {
			end = len(ids)
		}
		refs := []Ref{}
		for _, id := range ids[start:end] {
			refs = append(refs, Ref{ID: id})
		}
		next := ""
		if end < len(ids) || m.PageLoop {
			next = "page-2"
		}
		_ = json.NewEncoder(w).Encode(ListPage{Messages: refs, Next: next})
	case "/history":
		if m.Expired {
			m.Expired = false
			w.WriteHeader(404)
			return
		}
		entry := HistoryEntry{Messages: []Ref{{ID: "m1"}, {ID: "m2"}}}
		next := ""
		if m.HistoryPages && r.URL.Query().Get("pageToken") == "" {
			entry = HistoryEntry{Messages: []Ref{{ID: "m1"}}}
			next = "history-2"
		}
		if m.PageLoop {
			next = "history-2"
		}
		_ = json.NewEncoder(w).Encode(HistoryPage{Entries: []HistoryEntry{entry}, HistoryID: m.HistoryID, Next: next})
	default:
		id := strings.TrimPrefix(path, "/messages/")
		modify := strings.HasSuffix(id, "/modify")
		id = strings.TrimSuffix(id, "/modify")
		if m.FailMessage == id {
			w.WriteHeader(500)
			return
		}
		msg, ok := m.Messages[id]
		if !ok {
			w.WriteHeader(404)
			return
		}
		if modify {
			var data struct {
				Add    []string `json:"addLabelIds"`
				Remove []string `json:"removeLabelIds"`
			}
			_ = json.NewDecoder(r.Body).Decode(&data)
			set := map[string]bool{}
			for _, s := range msg.Labels {
				set[s] = true
			}
			for _, s := range data.Add {
				set[s] = true
			}
			for _, s := range data.Remove {
				delete(set, s)
			}
			msg.Labels = []string{}
			for s := range set {
				msg.Labels = append(msg.Labels, s)
			}
			m.Messages[id] = msg
		}
		_ = json.NewEncoder(w).Encode(msg)
	}
}
