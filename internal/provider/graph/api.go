package graph

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/duanyongcheng/pebble-go/internal/domain"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/mailnet"
)

const (
	metadataLimit = 4 << 20
	rawLimit      = 50 << 20
	// Graph's MIME submission has a smaller wire budget than our composer.
	sendWireLimit   = 4 << 20
	MaxSendRawBytes = sendWireLimit / 4 * 3
	maxLink         = 16384
)

var ErrCursorExpired = errors.New("Graph cursor expired")

type Identity struct{ ID, Email, DisplayName string }
type Folder struct {
	ID               string `json:"id"`
	DisplayName      string `json:"displayName"`
	ParentFolderID   string `json:"parentFolderId"`
	ChildFolderCount int    `json:"childFolderCount"`
}
type Flag struct {
	FlagStatus string `json:"flagStatus"`
}
type Message struct {
	ID                string     `json:"id"`
	ParentFolderID    string     `json:"parentFolderId"`
	IsRead            *bool      `json:"isRead"`
	IsDraft           *bool      `json:"isDraft"`
	Flag              *Flag      `json:"flag"`
	InternetMessageID string     `json:"internetMessageId"`
	From              *Recipient `json:"from"`
	Removed           bool       `json:"-"`
}

type Recipient struct {
	EmailAddress struct {
		Address string `json:"address"`
	} `json:"emailAddress"`
}

func (m *Message) UnmarshalJSON(b []byte) error {
	type plain Message
	v := struct {
		*plain
		Removed json.RawMessage `json:"@removed"`
	}{plain: (*plain)(m)}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	m.Removed = len(v.Removed) > 0 && string(v.Removed) != "null"
	return nil
}

type DeltaPage struct {
	Messages            []Message
	NextLink, DeltaLink string
}
type FolderPage struct {
	Folders  []Folder
	NextLink string
}

// API never refreshes, retries, redirects, or chooses a network policy. Its
// supplied client and token source already belong to one pinned account.
type API struct {
	client *http.Client
	token  func(context.Context) (string, error)
	base   string
}

func NewAPI(client *http.Client, token func(context.Context) (string, error)) *API {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	c := *client
	if c.Transport == nil {
		c.Transport = (mailnet.Dialer{}).HTTPTransport()
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	return &API{client: &c, token: token, base: "https://graph.microsoft.com/v1.0/me"}
}

// Session pins a single authorization lease across identity verification and
// submission. The caller must obtain it before recording its pre-send barrier.
func (a *API) Session(ctx context.Context) (*API, error) {
	if a.token == nil {
		return nil, errors.New("Graph token source unavailable")
	}
	token, err := a.token(ctx)
	if err != nil {
		var access *domain.AccessError
		if errors.As(err, &access) {
			return nil, access
		}
		return nil, errors.New("Graph authorization unavailable")
	}
	if token == "" || strings.ContainsAny(token, "\r\n\x00") {
		return nil, errors.New("invalid Graph authorization")
	}
	clone := *a
	clone.token = func(context.Context) (string, error) { return token, nil }
	return &clone, nil
}

// HTTPError deliberately excludes URLs, response bodies and bearer material.
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("Graph request rejected (HTTP %d)", e.Status) }

func validID(s string) bool {
	return s != "" && len(s) <= 2048 && s != "." && s != ".." && !strings.ContainsAny(s, "\\\r\n\x00") && strings.IndexFunc(s, func(r rune) bool { return r <= 32 || r == 127 }) < 0
}

func segment(s string) (string, error) {
	if !validID(s) {
		return "", errors.New("invalid Graph identifier")
	}
	return url.PathEscape(s), nil
}

// Saved and returned continuation URLs must bind to precisely this operation.
// The query remains opaque: decoding/re-encoding it can corrupt delta tokens.
func (a *API) link(link, route string) (string, error) {
	if len(link) == 0 || len(link) > maxLink || strings.ContainsAny(link, "\\\r\n\x00") {
		return "", errors.New("invalid Graph continuation")
	}
	u, err := url.Parse(link)
	b, berr := url.Parse(a.base)
	expectedPath, pathErr := url.PathUnescape(route)
	if err != nil || berr != nil || pathErr != nil || u.Scheme != "https" || u.Scheme != b.Scheme || u.Host != b.Host || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.EscapedPath() != b.EscapedPath()+route || u.Path != b.Path+expectedPath {
		return "", errors.New("unsafe Graph continuation")
	}
	return link, nil
}

// request reports whether an HTTP attempt began. Callers use that bit to
// quarantine writes when no definitive rejection/receipt can be established.
func (a *API) request(ctx context.Context, method, target, contentType string, body []byte, limit int64) ([]byte, int, bool, error) {
	if a.token == nil {
		return nil, 0, false, errors.New("Graph token source unavailable")
	}
	token, err := a.token(ctx)
	if err != nil {
		var access *domain.AccessError
		if errors.As(err, &access) {
			return nil, 0, false, access
		}
		return nil, 0, false, errors.New("Graph authorization unavailable")
	}
	if token == "" || strings.ContainsAny(token, "\r\n\x00") {
		return nil, 0, false, errors.New("invalid Graph authorization")
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, 0, false, errors.New("invalid Graph request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Prefer", `IdType="ImmutableId"`)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	// Prevent transports from replaying a non-idempotent body after a failure.
	if method != http.MethodGet {
		req.GetBody = nil
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, 0, true, errors.New("Graph transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusGone {
		return nil, resp.StatusCode, true, ErrCursorExpired
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, true, &HTTPError{Status: resp.StatusCode}
	}
	if resp.ContentLength > limit {
		return nil, resp.StatusCode, true, errors.New("Graph response exceeds limit")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, true, errors.New("Graph response interrupted")
	}
	if int64(len(b)) > limit {
		return nil, resp.StatusCode, true, errors.New("Graph response exceeds limit")
	}
	return b, resp.StatusCode, true, nil
}

func (a *API) get(ctx context.Context, route string, out any) error {
	b, _, _, err := a.request(ctx, http.MethodGet, a.base+route, "", nil, metadataLimit)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return errors.New("invalid Graph metadata")
	}
	return nil
}

func (a *API) Profile(ctx context.Context) (Identity, error) {
	var p struct {
		ID          string `json:"id"`
		Mail        string `json:"mail"`
		UPN         string `json:"userPrincipalName"`
		DisplayName string `json:"displayName"`
	}
	if err := a.get(ctx, "?$select=id,mail,userPrincipalName,displayName", &p); err != nil {
		return Identity{}, err
	}
	if p.Mail == "" {
		p.Mail = p.UPN
	}
	address, addressErr := mail.ParseAddress(p.Mail)
	if !validID(p.ID) || addressErr != nil || address.Address != p.Mail || strings.ContainsAny(p.Mail, "\r\n\x00") {
		return Identity{}, errors.New("Graph mailbox identity unavailable")
	}
	return Identity{ID: p.ID, Email: p.Mail, DisplayName: p.DisplayName}, nil
}

func (a *API) WellKnownFolder(ctx context.Context, alias string) (Folder, error) {
	switch alias {
	case "inbox", "sentitems", "drafts", "deleteditems", "junkemail", "archive":
	default:
		return Folder{}, errors.New("unsupported Graph system folder")
	}
	var f Folder
	if err := a.get(ctx, "/mailFolders/"+alias+"?$select=id,displayName,parentFolderId,childFolderCount", &f); err != nil {
		return Folder{}, err
	}
	if !validID(f.ID) || f.ChildFolderCount < 0 {
		return Folder{}, errors.New("invalid Graph system folder")
	}
	return f, nil
}

func (a *API) FolderPage(ctx context.Context, parent, link string) (FolderPage, error) {
	route := "/mailFolders"
	if parent != "" {
		id, err := segment(parent)
		if err != nil {
			return FolderPage{}, err
		}
		route += "/" + id + "/childFolders"
	}
	target := a.base + route + "?$top=50&includeHiddenFolders=true"
	if link != "" {
		var err error
		target, err = a.link(link, route)
		if err != nil {
			return FolderPage{}, err
		}
	}
	b, _, _, err := a.request(ctx, http.MethodGet, target, "", nil, metadataLimit)
	if err != nil {
		return FolderPage{}, err
	}
	var page struct {
		Value []Folder `json:"value"`
		Next  string   `json:"@odata.nextLink"`
	}
	if json.Unmarshal(b, &page) != nil || len(page.Value) > 1000 {
		return FolderPage{}, errors.New("invalid Graph folder page")
	}
	for _, f := range page.Value {
		if !validID(f.ID) || f.ChildFolderCount < 0 {
			return FolderPage{}, errors.New("invalid Graph folder")
		}
	}
	if page.Next != "" {
		if _, err = a.link(page.Next, route); err != nil {
			return FolderPage{}, err
		}
		if page.Next == target {
			return FolderPage{}, errors.New("Graph continuation loop")
		}
	}
	return FolderPage{Folders: page.Value, NextLink: page.Next}, nil
}

func (a *API) Delta(ctx context.Context, folder, link string) (DeltaPage, error) {
	id, err := segment(folder)
	if err != nil {
		return DeltaPage{}, err
	}
	route := "/mailFolders/" + id + "/messages/delta"
	target := a.base + route + "?$top=50&$select=id,parentFolderId,isRead,isDraft,flag,internetMessageId"
	if link != "" {
		target, err = a.link(link, route)
		if err != nil {
			return DeltaPage{}, err
		}
	}
	b, _, _, err := a.request(ctx, http.MethodGet, target, "", nil, metadataLimit)
	if err != nil {
		return DeltaPage{}, err
	}
	var page struct {
		Value []Message `json:"value"`
		Next  string    `json:"@odata.nextLink"`
		Delta string    `json:"@odata.deltaLink"`
	}
	if json.Unmarshal(b, &page) != nil || len(page.Value) > 1000 || (page.Next != "" && page.Delta != "") || (page.Next == "" && page.Delta == "") {
		return DeltaPage{}, errors.New("invalid Graph delta page")
	}
	for _, m := range page.Value {
		if !validID(m.ID) {
			return DeltaPage{}, errors.New("invalid Graph message")
		}
	}
	for _, s := range []string{page.Next, page.Delta} {
		if s != "" {
			if _, err = a.link(s, route); err != nil {
				return DeltaPage{}, err
			}
		}
	}
	if page.Next == target {
		return DeltaPage{}, errors.New("Graph continuation loop")
	}
	return DeltaPage{Messages: page.Value, NextLink: page.Next, DeltaLink: page.Delta}, nil
}

func (a *API) Raw(ctx context.Context, id string) ([]byte, error) {
	s, err := segment(id)
	if err != nil {
		return nil, err
	}
	b, _, _, err := a.request(ctx, http.MethodGet, a.base+"/messages/"+s+"/$value", "", nil, rawLimit)
	return b, err
}

func uncertain(status int, attempted bool) bool {
	return attempted && (status == 0 || status >= 500 || status >= 300 && status < 400 || status >= 200 && status < 300 || status == 408)
}

func (a *API) write(ctx context.Context, method, route string, payload any, expected int, out any) (bool, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return false, errors.New("invalid Graph write")
	}
	if payload == nil {
		b = nil
	}
	data, status, attempted, err := a.request(ctx, method, a.base+route, "application/json", b, metadataLimit)
	if err != nil {
		return uncertain(status, attempted), err
	}
	if status != expected {
		return true, errors.New("unexpected Graph write receipt")
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return true, errors.New("invalid Graph write receipt")
	}
	return false, nil
}

func (a *API) PatchFlags(ctx context.Context, id string, read, star *bool) (bool, error) {
	s, err := segment(id)
	if err != nil {
		return false, err
	}
	p := map[string]any{}
	if read != nil {
		p["isRead"] = *read
	}
	if star != nil {
		v := "notFlagged"
		if *star {
			v = "flagged"
		}
		p["flag"] = map[string]string{"flagStatus": v}
	}
	if len(p) == 0 {
		return false, errors.New("empty Graph flag change")
	}
	return a.write(ctx, http.MethodPatch, "/messages/"+s, p, http.StatusOK, nil)
}

func (a *API) Move(ctx context.Context, id, destination string) (string, bool, error) {
	s, err := segment(id)
	if err != nil {
		return "", false, err
	}
	if !validID(destination) {
		return "", false, errors.New("invalid Graph destination")
	}
	var receipt Message
	unknown, err := a.write(ctx, http.MethodPost, "/messages/"+s+"/move", map[string]string{"destinationId": destination}, http.StatusCreated, &receipt)
	if err == nil && !validID(receipt.ID) {
		return "", true, errors.New("missing Graph move receipt")
	}
	return receipt.ID, unknown, err
}

func validName(s string) bool {
	return strings.TrimSpace(s) != "" && len(s) <= 1024 && !strings.ContainsAny(s, "\r\n\x00")
}
func (a *API) CreateFolder(ctx context.Context, parent, name string) (Folder, bool, error) {
	if !validName(name) {
		return Folder{}, false, errors.New("invalid Graph folder name")
	}
	route := "/mailFolders"
	if parent != "" {
		s, err := segment(parent)
		if err != nil {
			return Folder{}, false, err
		}
		route += "/" + s + "/childFolders"
	}
	var f Folder
	unknown, err := a.write(ctx, http.MethodPost, route, map[string]string{"displayName": name}, http.StatusCreated, &f)
	if err == nil && !validID(f.ID) {
		return Folder{}, true, errors.New("missing Graph folder receipt")
	}
	return f, unknown, err
}
func (a *API) RenameFolder(ctx context.Context, id, name string) (bool, error) {
	s, err := segment(id)
	if err != nil {
		return false, err
	}
	if !validName(name) {
		return false, errors.New("invalid Graph folder name")
	}
	return a.write(ctx, http.MethodPatch, "/mailFolders/"+s, map[string]string{"displayName": name}, http.StatusOK, nil)
}
func (a *API) DeleteFolder(ctx context.Context, id string) (bool, error) {
	s, err := segment(id)
	if err != nil {
		return false, err
	}
	return a.write(ctx, http.MethodDelete, "/mailFolders/"+s, nil, http.StatusNoContent, nil)
}

func (a *API) Send(ctx context.Context, raw []byte) (accepted, unknown, retryable bool, err error) {
	if len(raw) == 0 || base64.StdEncoding.EncodedLen(len(raw)) > sendWireLimit {
		return false, false, false, errors.New("Graph MIME submission exceeds supported limit")
	}
	body := []byte(base64.StdEncoding.EncodeToString(raw))
	_, status, attempted, err := a.request(ctx, http.MethodPost, a.base+"/sendMail", "text/plain", body, metadataLimit)
	if err == nil && status == http.StatusAccepted {
		return true, false, false, nil
	}
	if err == nil {
		err = errors.New("unexpected Graph send receipt")
	}
	unknown = uncertain(status, attempted)
	return false, unknown, !unknown && (status == 401 || status == 403 || status == 429), err
}

// FindSent must exhaust all bounded pages before returning any matches. An
// incomplete check must never turn an unknown submission into a sent receipt.
func (a *API) FindSent(ctx context.Context, messageID string) ([]Message, error) {
	if messageID == "" || len(messageID) > 998 || strings.ContainsAny(messageID, "\r\n\x00") {
		return nil, errors.New("invalid sent-check Message-ID")
	}
	route := "/mailFolders/sentitems/messages"
	q := url.Values{"$filter": {"internetMessageId eq '" + strings.ReplaceAll(messageID, "'", "''") + "'"}, "$select": {"id,parentFolderId,internetMessageId,isDraft,from"}, "$top": {"50"}}
	target := a.base + route + "?" + q.Encode()
	seen := map[string]bool{}
	var result []Message
	for count := 0; count < 100; count++ {
		if seen[target] {
			return nil, errors.New("Graph sent-check continuation loop")
		}
		seen[target] = true
		b, _, _, err := a.request(ctx, http.MethodGet, target, "", nil, metadataLimit)
		if err != nil {
			return nil, err
		}
		var p struct {
			Value []Message `json:"value"`
			Next  string    `json:"@odata.nextLink"`
		}
		if json.Unmarshal(b, &p) != nil || len(p.Value) > 1000 {
			return nil, errors.New("invalid Graph sent-check page")
		}
		for _, m := range p.Value {
			if !validID(m.ID) || m.InternetMessageID != messageID || m.Removed || m.IsDraft != nil && *m.IsDraft {
				return nil, errors.New("invalid Graph sent-check match")
			}
			result = append(result, m)
			if len(result) > 1000 {
				return nil, errors.New("Graph sent-check limit exceeded")
			}
		}
		if p.Next == "" {
			return result, nil
		}
		target, err = a.link(p.Next, route)
		if err != nil {
			return nil, err
		}
	}
	return nil, errors.New("Graph sent-check page limit exceeded")
}
