package googleapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

const APIBase = "https://gmail.googleapis.com/gmail/v1/users/me"
const MaxRawMessageBytes = 50 << 20
const defaultResponseLimit = 4 << 20
const rawResponseLimit = ((MaxRawMessageBytes+2)/3)*4 + (1 << 20)

// ContentError identifies bounded message content failures, without retaining
// remote payloads or parser errors that could include message contents.
type ContentError struct{ Kind string }

func (e *ContentError) Error() string { return "Gmail message content unavailable: " + e.Kind }

type TokenSource interface {
	Token(context.Context) (string, error)
}
type API struct {
	Base          string
	HTTP          *http.Client
	Tokens        TokenSource
	BeforeRequest func() error
	OnResult      func(error)
}
type HTTPError struct {
	Status       int
	Reasons      []string
	googleStatus string
	responseKind string
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

// Gmail answers with two shapes: the legacy errors[].reason list and the newer
// error.status code. Neither is guaranteed, so both are decoded and classified.
func decodeHTTPError(status int, body []byte) *HTTPError {
	var value struct {
		Error struct {
			Status  string `json:"status"`
			Details []struct {
				Reason string `json:"reason"`
			} `json:"details"`
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	out := &HTTPError{Status: status, responseKind: "json"}
	if len(bytes.TrimSpace(body)) == 0 {
		out.responseKind = "empty"
	} else if json.Unmarshal(body, &value) != nil {
		out.responseKind = "non_json"
	}
	out.googleStatus = value.Error.Status
	for _, e := range value.Error.Errors {
		out.Reasons = append(out.Reasons, e.Reason)
	}
	for _, e := range value.Error.Details {
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
	return a.requestWithLimit(ctx, method, path, query, body, out, defaultResponseLimit)
}

func (a *API) requestWithLimit(ctx context.Context, method, path string, query url.Values, body any, out any, limit int64) (result error) {
	defer func() { a.report(result) }()
	started := time.Now()
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
	retries, authRetries := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.checkAccess(); err != nil {
			return err
		}
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
		if err := a.checkAccess(); err != nil {
			return err
		}
		resp, err := a.HTTP.Do(req)
		if err != nil {
			slog.Warn("Gmail request failed", "operation", requestOperation(path), "stage", "request", "kind", requestErrorKind(err), "elapsed_ms", time.Since(started).Milliseconds())
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &domain.AccessError{State: domain.ConnectionTemporaryError}
		}
		responseLimit := limit
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			responseLimit = defaultResponseLimit
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && resp.ContentLength > responseLimit {
			_ = resp.Body.Close()
			return &ContentError{Kind: "too_large"}
		}
		payload, readErr := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
		_ = resp.Body.Close()
		if readErr != nil {
			slog.Warn("Gmail request failed", "operation", requestOperation(path), "stage", "response_body", "kind", requestErrorKind(readErr), "status", resp.StatusCode, "elapsed_ms", time.Since(started).Milliseconds())
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return &domain.AccessError{State: domain.ConnectionTemporaryError}
			}
		}
		if int64(len(payload)) > responseLimit && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return &ContentError{Kind: "too_large"}
		}
		// Refresh once per request. Counting retries rather than loop iterations
		// keeps the refresh actionable even when a read was retried first.
		if method == http.MethodGet && resp.StatusCode == 401 && authRetries == 0 {
			authRetries++
			if tokens, ok := a.Tokens.(interface{ Invalidate(string) }); ok {
				tokens.Invalidate(token)
				continue
			}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			remote := decodeHTTPError(resp.StatusCode, payload)
			if method == http.MethodGet && retryableHTTP(remote) && retries < maxReadRetries {
				now := time.Now()
				delay := readRetryDelay(retries, resp.Header.Get("Retry-After"), now)
				// A longer server cooldown must not be shortened to fit our budget.
				if delay <= maxReadRetryElapsed-now.Sub(started) {
					retries++
					slog.Info("Gmail request retry scheduled", "operation", requestOperation(path), "status", remote.Status, "reason", requestReason(remote), "retry", retries, "delay_ms", delay.Milliseconds())
					timer := time.NewTimer(delay)
					select {
					case <-ctx.Done():
						timer.Stop()
						return ctx.Err()
					case <-timer.C:
					}
					continue
				}
			}
			retryAfter, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			slog.Warn("Gmail request failed", "operation", requestOperation(path), "stage", "http_status", "status", resp.StatusCode, "reason", requestReason(remote), "google_status", requestGoogleStatus(remote), "response_kind", remote.responseKind, "retry_after_seconds", retryAfter, "elapsed_ms", time.Since(started).Milliseconds())
			return remote
		}
		if out == nil {
			return nil
		}
		if json.Unmarshal(payload, out) != nil {
			// A 2xx body that stops mid-object is a cut stream, not bad mail.
			// It is reported as transport-shaped so callers retry the request
			// instead of quarantining the message as undecodable content.
			// Only a body that started as an object but stopped early qualifies;
			// prose or HTML from an intermediary stays a content failure.
			cut := len(bytes.TrimSpace(payload)) > 0 && bytes.TrimSpace(payload)[0] == '{' && !json.Valid(payload)
			if resp.StatusCode >= 200 && resp.StatusCode < 300 && cut && readErr == nil {
				slog.Warn("Gmail response body was incomplete", "operation", requestOperation(path), "stage", "decode", "elapsed_ms", time.Since(started).Milliseconds())
				return &domain.AccessError{State: domain.ConnectionTemporaryError}
			}
			return &ContentError{Kind: "invalid_content"}
		}
		return nil
	}
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
	if err == nil {
		err = ValidateLabels(p.Labels)
	}
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
	err := a.requestWithLimit(ctx, "GET", "/messages/"+url.PathEscape(id), url.Values{"format": {"raw"}}, nil, &m, rawResponseLimit)
	if err == nil && (m.ID != id || m.Raw == "") {
		err = &ContentError{Kind: "invalid_content"}
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
