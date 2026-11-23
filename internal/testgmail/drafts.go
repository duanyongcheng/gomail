package testgmail

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"sort"
	"strconv"
	"strings"

	"github.com/duanyongcheng/gomail/internal/googleapi"
)

// SetDraft seeds or externally edits a remote draft without counting an API write.
func (m *Mock) SetDraft(id string, raw []byte, threadID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Drafts == nil {
		m.Drafts = map[string]googleapi.Draft{}
	}
	m.draftRevision++
	messageID := fmt.Sprintf("draft-message-%d", m.draftRevision)
	m.Drafts[id] = googleapi.Draft{ID: id, Message: Message{
		ID: messageID, ThreadID: threadID, Labels: []string{"DRAFT"},
		Raw: base64.RawURLEncoding.EncodeToString(raw), Date: "1700000000000",
	}}
	return messageID
}

// serveDraft runs with Mock.mu held. Used only by local tests/selftest.
func (m *Mock) serveDraft(w http.ResponseWriter, r *http.Request, path string) {
	if m.Drafts == nil {
		m.Drafts = map[string]googleapi.Draft{}
	}
	id := strings.TrimPrefix(path, "/drafts/")
	switch r.Method {
	case http.MethodGet:
		if path == "/drafts" {
			ids := []string{}
			for _, d := range m.Drafts {
				if query := r.URL.Query().Get("q"); query != "" {
					raw, _ := base64.RawURLEncoding.DecodeString(d.Message.Raw)
					parsed, err := mail.ReadMessage(bytes.NewReader(raw))
					if err != nil || strings.Trim(parsed.Header.Get("Message-ID"), "<>") != strings.TrimPrefix(query, "rfc822msgid:") {
						continue
					}
				}
				ids = append(ids, d.ID)
			}
			sort.Strings(ids)
			start := 0
			if token := r.URL.Query().Get("pageToken"); token != "" {
				var err error
				start, err = strconv.Atoi(token)
				if err != nil || start < 0 || start > len(ids) {
					w.WriteHeader(400)
					return
				}
			}
			size := m.DraftPageSize
			if size <= 0 {
				size = 20
			}
			if requested, err := strconv.Atoi(r.URL.Query().Get("maxResults")); err == nil && requested > 0 && requested < size {
				size = requested
			}
			end := min(start+size, len(ids))
			result := googleapi.DraftPage{Drafts: []googleapi.Draft{}}
			for _, id := range ids[start:end] {
				d := m.Drafts[id]
				result.Drafts = append(result.Drafts, googleapi.Draft{ID: d.ID, Message: Message{ID: d.Message.ID, ThreadID: d.Message.ThreadID}})
			}
			if end < len(ids) {
				result.Next = strconv.Itoa(end)
			}
			if m.PageLoop {
				result.Next = "1"
			}
			_ = json.NewEncoder(w).Encode(result)
			return
		}
		d, ok := m.Drafts[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("format") == "metadata" {
			var metadata googleapi.DraftMetadata
			metadata.ID = d.ID
			metadata.Message.ID, metadata.Message.ThreadID = d.Message.ID, d.Message.ThreadID
			metadata.Message.Date, metadata.Message.Labels = d.Message.Date, d.Message.Labels
			raw, _ := base64.RawURLEncoding.DecodeString(d.Message.Raw)
			if parsed, err := mail.ReadMessage(bytes.NewReader(raw)); err == nil {
				for name, values := range parsed.Header {
					for _, value := range values {
						metadata.Message.Payload.Headers = append(metadata.Message.Payload.Headers, googleapi.DraftHeader{Name: name, Value: value})
					}
				}
			}
			_ = json.NewEncoder(w).Encode(metadata)
		} else {
			_ = json.NewEncoder(w).Encode(d)
		}
	case http.MethodDelete:
		m.DraftDeletes++
		if m.DraftDeleteStatus != 0 {
			w.WriteHeader(m.DraftDeleteStatus)
			return
		}
		if _, ok := m.Drafts[id]; !ok {
			http.NotFound(w, r)
			return
		}
		delete(m.Drafts, id)
		if m.LoseDraftDeleteResponse {
			loseResponse(w)
			return
		}
		_ = json.NewEncoder(w).Encode(struct{}{})
	case http.MethodPost, http.MethodPut:
		var d googleapi.Draft
		if json.NewDecoder(io.LimitReader(r.Body, 50<<20)).Decode(&d) != nil || d.Message.Raw == "" {
			w.WriteHeader(400)
			return
		}
		if path == "/drafts/send" && r.Method == http.MethodPost {
			m.DraftSends++
			if _, ok := m.Drafts[d.ID]; !ok {
				http.NotFound(w, r)
				return
			}
			m.recordSent(w, d.Message.Raw, d.Message.ThreadID, d.ID)
			return
		}
		if r.Method == http.MethodPost {
			if path != "/drafts" {
				w.WriteHeader(405)
				return
			}
			for {
				m.draftSequence++
				id = fmt.Sprintf("draft-%d", m.draftSequence)
				if _, exists := m.Drafts[id]; !exists {
					break
				}
			}
		} else {
			if _, ok := m.Drafts[id]; !ok {
				http.NotFound(w, r)
				return
			}
		}
		m.DraftWrites++
		m.draftRevision++
		d.ID = id
		d.Message.ID = fmt.Sprintf("draft-message-%d", m.draftRevision)
		d.Message.Labels = []string{"DRAFT"}
		d.Message.Date = "1700000000000"
		m.Drafts[id] = d
		_ = json.NewEncoder(w).Encode(googleapi.Draft{ID: d.ID, Message: Message{ID: d.Message.ID}})
	default:
		w.WriteHeader(405)
	}
}

func loseResponse(w http.ResponseWriter) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

// recordSent runs under Mock.mu and consumes a draft only on Gmail acceptance.
func (m *Mock) recordSent(w http.ResponseWriter, encoded, thread, draftID string) {
	if m.SendStatus != 0 {
		w.WriteHeader(m.SendStatus)
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) == 0 {
		w.WriteHeader(400)
		return
	}
	m.SentRaw = append(m.SentRaw, raw)
	id := fmt.Sprintf("sent-%d", len(m.SentRaw))
	if thread == "" {
		thread = "thread-" + id
	}
	m.Messages[id] = Message{ID: id, ThreadID: thread, Labels: []string{"SENT"}, Raw: encoded, Date: "1700000000000", HistoryID: m.HistoryID}
	if draftID != "" {
		delete(m.Drafts, draftID)
	}
	if m.LoseSendResponse {
		loseResponse(w)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}
