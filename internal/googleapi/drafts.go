package googleapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/mailmime"
)

// A draft's stable ID is different from its replaceable message ID.
type Draft struct {
	ID      string  `json:"id"`
	Message Message `json:"message"`
}
type DraftPage struct {
	Drafts []Draft `json:"drafts"`
	Next   string  `json:"nextPageToken"`
}
type DraftWriteResult struct {
	Draft   Draft
	Unknown bool
}

const DraftPageSize = 20

type DraftHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type DraftMetadata struct {
	ID      string `json:"id"`
	Message struct {
		ID       string   `json:"id"`
		ThreadID string   `json:"threadId"`
		Labels   []string `json:"labelIds"`
		Date     string   `json:"internalDate"`
		Payload  struct {
			Headers []DraftHeader `json:"headers"`
		} `json:"payload"`
	} `json:"message"`
}

func validDraftID(id string) bool {
	if id == "" || len(id) > 512 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func validDraftPageToken(token string) bool {
	if len(token) > 4096 || !utf8.ValidString(token) {
		return false
	}
	for _, r := range token {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (a *API) ListDrafts(ctx context.Context, pageToken string) (DraftPage, error) {
	var page DraftPage
	if !validDraftPageToken(pageToken) {
		return page, errors.New("invalid Gmail draft page token")
	}
	query := url.Values{"maxResults": {"20"}}
	if pageToken != "" {
		query.Set("pageToken", pageToken)
	}
	if err := a.request(ctx, http.MethodGet, "/drafts", query, nil, &page); err != nil {
		return DraftPage{}, err
	}
	if err := validateDraftPage(page, DraftPageSize); err != nil || (page.Next != "" && page.Next == pageToken) {
		return DraftPage{}, &ContentError{Kind: "invalid_content"}
	}
	return page, nil
}

func validateDraftPage(page DraftPage, limit int) error {
	if len(page.Drafts) > limit || !validDraftPageToken(page.Next) {
		return &ContentError{Kind: "invalid_content"}
	}
	seen := map[string]bool{}
	for _, draft := range page.Drafts {
		if !validDraftID(draft.ID) || !validDraftID(draft.Message.ID) || seen[draft.ID] {
			return &ContentError{Kind: "invalid_content"}
		}
		seen[draft.ID] = true
	}
	return nil
}

// DraftMetadata keeps paginated browsing independent of full MIME/attachment size.
func (a *API) DraftMetadata(ctx context.Context, id string) (DraftMetadata, error) {
	var d DraftMetadata
	if !validDraftID(id) {
		return d, errors.New("invalid Gmail draft ID")
	}
	err := a.request(ctx, http.MethodGet, "/drafts/"+id, url.Values{"format": {"metadata"}}, nil, &d)
	if err == nil && (d.ID != id || !validDraftID(d.Message.ID)) {
		err = &ContentError{Kind: "invalid_content"}
	}
	return d, err
}

func (a *API) Draft(ctx context.Context, id string) (Draft, error) {
	var d Draft
	if !validDraftID(id) {
		return d, errors.New("invalid Gmail draft ID")
	}
	err := a.requestWithLimit(ctx, http.MethodGet, "/drafts/"+id, url.Values{"format": {"raw"}}, nil, &d, rawResponseLimit)
	if err == nil && (d.ID != id || !validDraftID(d.Message.ID) || d.Message.Raw == "") {
		err = &ContentError{Kind: "invalid_content"}
	}
	return d, err
}
func (a *API) FindDraft(ctx context.Context, messageID string) (DraftPage, error) {
	var page DraftPage
	if len(messageID) > 200 || !strings.HasSuffix(messageID, "@pebble.invalid") || strings.ContainsAny(messageID, " \r\n\t<>:") {
		return page, errors.New("invalid draft search identity")
	}
	err := a.request(ctx, http.MethodGet, "/drafts", url.Values{"q": {"rfc822msgid:" + messageID}, "maxResults": {"10"}}, nil, &page)
	if err == nil {
		if err := validateDraftPage(page, 10); err != nil {
			return DraftPage{}, err
		}
	}
	return page, err
}

// DeleteDraft issues one destructive request. Even an idempotent-looking DELETE
// is not replayed: a retry could remove a draft changed by another client.
func (a *API) DeleteDraft(ctx context.Context, id string) (DraftWriteResult, error) {
	var result DraftWriteResult
	if !validDraftID(id) {
		return result, errors.New("invalid Gmail draft ID")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := a.checkAccess(); err != nil {
		return result, err
	}
	token, err := a.Tokens.Token(ctx)
	if err != nil {
		a.report(err)
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, a.Base+"/drafts/"+id, nil)
	if err != nil {
		return result, errors.New("invalid Gmail draft endpoint")
	}
	req.GetBody = nil
	req.Header.Set("Authorization", "Bearer "+token)
	client := *a.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if err := a.checkAccess(); err != nil {
		return result, err
	}
	resp, err := client.Do(req)
	if err != nil {
		a.report(&domain.AccessError{State: domain.ConnectionTemporaryError})
		return DraftWriteResult{Unknown: true}, errors.New("Gmail draft deletion is unconfirmed; check the cloud copy")
	}
	defer resp.Body.Close()
	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, defaultResponseLimit+1))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := decodeHTTPError(resp.StatusCode, payload)
		a.report(e)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 408 {
			return result, e
		}
	}
	validBody := len(bytes.TrimSpace(payload)) == 0
	if !validBody {
		var empty map[string]json.RawMessage
		validBody = json.Unmarshal(payload, &empty) == nil && empty != nil && len(empty) == 0
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || readErr != nil || len(payload) > defaultResponseLimit || !validBody {
		return DraftWriteResult{Unknown: true}, errors.New("Gmail draft deletion is unconfirmed; check the cloud copy")
	}
	a.report(nil)
	return result, nil
}

// WriteDraft never retries or follows redirects. Unknown outcomes require a
// read-only reconciliation, including PUT: repeating it can overwrite web edits.
func (a *API) WriteDraft(ctx context.Context, id string, raw []byte, threadID string) (DraftWriteResult, error) {
	var result DraftWriteResult
	if (id != "" && !validDraftID(id)) || len(raw) == 0 || len(raw) > mailmime.MaxOutgoingBytes {
		return result, errors.New("invalid Gmail draft request")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := a.checkAccess(); err != nil {
		return result, err
	}
	token, err := a.Tokens.Token(ctx)
	if err != nil {
		a.report(err)
		return result, err
	}
	body, err := json.Marshal(struct {
		Message struct {
			Raw      string `json:"raw"`
			ThreadID string `json:"threadId,omitempty"`
		} `json:"message"`
	}{Message: struct {
		Raw      string `json:"raw"`
		ThreadID string `json:"threadId,omitempty"`
	}{base64.RawURLEncoding.EncodeToString(raw), threadID}})
	if err != nil {
		return result, errors.New("cannot encode Gmail draft")
	}
	method, path := http.MethodPost, "/drafts"
	if id != "" {
		method, path = http.MethodPut, path+"/"+id
	}
	req, err := http.NewRequestWithContext(ctx, method, a.Base+path, bytes.NewReader(body))
	if err != nil {
		return result, errors.New("invalid Gmail draft endpoint")
	}
	req.GetBody = nil
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := *a.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if err := a.checkAccess(); err != nil {
		return result, err
	}
	resp, err := client.Do(req)
	if err != nil {
		a.report(&domain.AccessError{State: domain.ConnectionTemporaryError})
		return DraftWriteResult{Unknown: true}, errors.New("Gmail draft outcome is unknown; check the cloud copy before retrying")
	}
	defer resp.Body.Close()
	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, defaultResponseLimit+1))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := decodeHTTPError(resp.StatusCode, payload)
		a.report(e)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 408 {
			return result, e
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || readErr != nil || len(payload) > defaultResponseLimit || json.Unmarshal(payload, &result.Draft) != nil || !validDraftID(result.Draft.ID) || !validDraftID(result.Draft.Message.ID) || (id != "" && result.Draft.ID != id) {
		return DraftWriteResult{Unknown: true}, errors.New("Gmail draft outcome is unknown; check the cloud copy before retrying")
	}
	a.report(nil)
	return result, nil
}
