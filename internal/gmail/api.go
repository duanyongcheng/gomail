package gmail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const APIBase = "https://gmail.googleapis.com/gmail/v1/users/me"

type TokenSource interface {
	Token(context.Context) (string, error)
}
type API struct {
	Base   string
	HTTP   *http.Client
	Tokens TokenSource
}
type HTTPError struct {
	Status  int
	Reasons []string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("Gmail API returned HTTP %d", e.Status) }
func (e *HTTPError) hasQuotaReason() bool {
	for _, r := range e.Reasons {
		if r == "rateLimitExceeded" || r == "userRateLimitExceeded" || r == "backendError" {
			return true
		}
	}
	return false
}
func googleQuotaReason(body []byte) bool {
	var value struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &value) != nil {
		return false
	}
	for _, e := range value.Error.Errors {
		if e.Reason == "rateLimitExceeded" || e.Reason == "userRateLimitExceeded" || e.Reason == "backendError" {
			return true
		}
	}
	return false
}
func decodeHTTPError(status int, body []byte) error {
	var value struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &value)
	out := &HTTPError{Status: status}
	for _, e := range value.Error.Errors {
		out.Reasons = append(out.Reasons, e.Reason)
	}
	return out
}

type Profile struct {
	Email     string `json:"emailAddress"`
	HistoryID string `json:"historyId"`
}
type Label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}
type Ref struct {
	ID string `json:"id"`
}
type Message struct {
	ID        string   `json:"id"`
	ThreadID  string   `json:"threadId"`
	Labels    []string `json:"labelIds"`
	Raw       string   `json:"raw"`
	Date      string   `json:"internalDate"`
	HistoryID string   `json:"historyId"`
}
type ListPage struct {
	Messages []Ref  `json:"messages"`
	Next     string `json:"nextPageToken"`
}
type HistoryEntry struct {
	Messages []Ref `json:"messages"`
	Added    []struct {
		Message Ref `json:"message"`
	} `json:"messagesAdded"`
	Deleted []struct {
		Message Ref `json:"message"`
	} `json:"messagesDeleted"`
	LabelsAdded []struct {
		Message Ref `json:"message"`
	} `json:"labelsAdded"`
	LabelsRemoved []struct {
		Message Ref `json:"message"`
	} `json:"labelsRemoved"`
}
type HistoryPage struct {
	Entries   []HistoryEntry `json:"history"`
	Next      string         `json:"nextPageToken"`
	HistoryID string         `json:"historyId"`
}

func (a *API) request(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	endpoint, err := url.Parse(a.Base + path)
	if err != nil {
		return err
	}
	endpoint.RawQuery = query.Encode()
	var data []byte
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		token, err := a.Tokens.Token(ctx)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := a.HTTP.Do(req)
		if err != nil {
			return errors.New("Gmail network request failed")
		}
		payload, readErr := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
		_ = resp.Body.Close()
		if readErr != nil {
			return errors.New("Gmail response read failed")
		}
		if len(payload) > 4<<20 {
			return errors.New("Gmail response exceeds prototype limit")
		}
		if method == http.MethodGet && resp.StatusCode == 401 && attempt == 0 {
			if tokens, ok := a.Tokens.(interface{ Invalidate(string) }); ok {
				tokens.Invalidate(token)
				continue
			}
		}
		if method == http.MethodGet && (resp.StatusCode == 429 || resp.StatusCode >= 500 || (resp.StatusCode == 403 && googleQuotaReason(payload))) && attempt < 2 {
			delay := time.Duration(attempt+1) * 100 * time.Millisecond
			if n, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && n > 0 && n <= 5 {
				delay = time.Duration(n) * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return decodeHTTPError(resp.StatusCode, payload)
		}
		if out == nil {
			return nil
		}
		if json.Unmarshal(payload, out) != nil {
			return errors.New("Gmail response JSON invalid")
		}
		return nil
	}
	return errors.New("Gmail retry budget exhausted")
}
func (a *API) Profile(ctx context.Context) (Profile, error) {
	var p Profile
	err := a.request(ctx, "GET", "/profile", nil, nil, &p)
	if err == nil && (p.Email == "" || p.HistoryID == "") {
		err = errors.New("Gmail profile incomplete")
	}
	return p, err
}
func (a *API) Labels(ctx context.Context) ([]Label, error) {
	var p struct {
		Labels []Label `json:"labels"`
	}
	err := a.request(ctx, "GET", "/labels", nil, nil, &p)
	return p.Labels, err
}
func (a *API) List(ctx context.Context, page string, max int) (ListPage, error) {
	var p ListPage
	q := url.Values{"maxResults": {strconv.Itoa(max)}, "includeSpamTrash": {"true"}}
	if page != "" {
		q.Set("pageToken", page)
	}
	err := a.request(ctx, "GET", "/messages", q, nil, &p)
	return p, err
}
func (a *API) Message(ctx context.Context, id string) (Message, error) {
	var m Message
	err := a.request(ctx, "GET", "/messages/"+url.PathEscape(id), url.Values{"format": {"raw"}}, nil, &m)
	if err == nil && (m.ID != id || m.Raw == "") {
		err = errors.New("Gmail message response incomplete")
	}
	return m, err
}
func (a *API) History(ctx context.Context, start, page string) (HistoryPage, error) {
	var p HistoryPage
	q := url.Values{"startHistoryId": {start}, "maxResults": {"100"}}
	if page != "" {
		q.Set("pageToken", page)
	}
	err := a.request(ctx, "GET", "/history", q, nil, &p)
	return p, err
}
func (a *API) Modify(ctx context.Context, id string, add, remove []string) error {
	var out Message
	return a.request(ctx, "POST", "/messages/"+url.PathEscape(id)+"/modify", nil, map[string]any{"addLabelIds": add, "removeLabelIds": remove}, &out)
}
