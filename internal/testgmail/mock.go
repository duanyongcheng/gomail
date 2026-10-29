package testgmail

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
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
	HistoryRefs       []Ref
	HistoryPageSize   int
	RequestURIs       []string
	FailMessage       string
	ExpectedChallenge string
	PageSize          int
	OmitFromList      map[string]bool
	Requests          []string
	ProfileEmail      string
	SentRaw           [][]byte
	SendStatus        int
	LoseSendResponse  bool
	LabelItems        map[string]Label
	LabelWrites       int
	LoseLabelResponse bool
	LabelWriteStatus  int
}

func (m *Mock) Lock()   { m.mu.Lock() }
func (m *Mock) Unlock() { m.mu.Unlock() }

func NewMock() *Mock {
	m := &Mock{Messages: map[string]Message{}, HistoryID: "900719925474099312345", LabelItems: map[string]Label{
		"INBOX": {ID: "INBOX", Name: "INBOX", Type: "system"}, "UNREAD": {ID: "UNREAD", Name: "UNREAD", Type: "system"},
		"STARRED": {ID: "STARRED", Name: "STARRED", Type: "system"}, "Label_work": {ID: "Label_work", Name: "工作", Type: "user"},
	}}
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
	m.RequestURIs = append(m.RequestURIs, r.URL.RequestURI())
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
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "demo-access", "refresh_token": "demo-refresh", "token_type": "Bearer", "expires_in": 3600, "scope": ModifyScope})
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
	if (path == "/labels" && r.Method == http.MethodPost) || strings.HasPrefix(path, "/labels/") {
		m.serveLabelWrite(w, r, path)
		return
	}
	switch path {
	case "/profile":
		email := m.ProfileEmail
		if email == "" {
			email = "demo@gmail.example.invalid"
		}
		_ = json.NewEncoder(w).Encode(Profile{Email: email, HistoryID: m.HistoryID})
	case "/labels":
		labels := []Label{}
		for _, label := range m.LabelItems {
			labels = append(labels, label)
		}
		sort.Slice(labels, func(i, j int) bool { return labels[i].ID < labels[j].ID })
		_ = json.NewEncoder(w).Encode(map[string]any{"labels": labels})
	case "/messages/send":
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		if m.SendStatus != 0 {
			w.WriteHeader(m.SendStatus)
			return
		}
		var input struct {
			Raw      string `json:"raw"`
			ThreadID string `json:"threadId"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 50<<20)).Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		raw, err := base64.RawURLEncoding.DecodeString(input.Raw)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		m.SentRaw = append(m.SentRaw, raw)
		id := fmt.Sprintf("sent-%d", len(m.SentRaw))
		thread := input.ThreadID
		if thread == "" {
			thread = "thread-" + id
		}
		m.Messages[id] = Message{ID: id, ThreadID: thread, Labels: []string{"SENT"}, Raw: input.Raw, Date: "1700000000000", HistoryID: m.HistoryID}
		if m.LoseSendResponse {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
	case "/messages":
		ids := make([]string, 0, len(m.Messages))
		for id, message := range m.Messages {
			if query := r.URL.Query().Get("q"); query != "" {
				data, _ := base64.RawURLEncoding.DecodeString(message.Raw)
				parsed, err := mail.ReadMessage(bytes.NewReader(data))
				if err != nil || strings.Trim(parsed.Header.Get("Message-ID"), "<>") != strings.TrimPrefix(query, "rfc822msgid:") {
					continue
				}
				sent := false
				for _, label := range message.Labels {
					if label == "SENT" {
						sent = true
					}
				}
				if !sent {
					continue
				}
			}
			if !m.OmitFromList[id] {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		page := r.URL.Query().Get("pageToken")
		start := 0
		if page != "" {
			var err error
			start, err = strconv.Atoi(page)
			if err != nil || start < 0 || start > len(ids) {
				w.WriteHeader(400)
				return
			}
		}
		size := m.PageSize
		if size <= 0 {
			size = 1
		}
		if requested, err := strconv.Atoi(r.URL.Query().Get("maxResults")); err == nil && requested < size {
			size = requested
		}
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		refs := []Ref{}
		for _, id := range ids[start:end] {
			refs = append(refs, Ref{ID: id})
		}
		next := ""
		if end < len(ids) {
			next = strconv.Itoa(end)
		}
		if m.PageLoop {
			next = "1"
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
		if m.HistoryRefs != nil {
			start := 0
			if token := r.URL.Query().Get("pageToken"); token != "" {
				var err error
				start, err = strconv.Atoi(token)
				if err != nil || start < 0 || start > len(m.HistoryRefs) {
					w.WriteHeader(400)
					return
				}
			}
			size := m.HistoryPageSize
			if size <= 0 {
				size = 100
			}
			end := min(start+size, len(m.HistoryRefs))
			entry.Messages = m.HistoryRefs[start:end]
			if end < len(m.HistoryRefs) {
				next = strconv.Itoa(end)
			}
		}
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
