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
)

type fixedToken string

func (t fixedToken) Token(context.Context) (string, error) { return string(t), nil }

// SendingSession pins one token across the profile check and submission. Never
// mutate the shared HTTP client or follow a redirect carrying a message body.
func (a *API) SendingSession(ctx context.Context) (*API, error) {
	token, err := a.Tokens.Token(ctx)
	if err != nil {
		return nil, errors.New("Gmail sign-in required")
	}
	client := *a.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &API{Base: a.Base, HTTP: &client, Tokens: fixedToken(token)}, nil
}

type SendResponse struct {
	RemoteID           string
	Unknown, Retryable bool
	Message            string
}

// SendRaw does exactly one POST. A transport/read/malformed success/5xx/408
// failure cannot prove Gmail did not accept the message, so it is not retryable.
func (a *API) SendRaw(ctx context.Context, raw []byte) SendResponse {
	if err := ctx.Err(); err != nil {
		return SendResponse{Retryable: true, Message: "Send cancelled before submission."}
	}
	body, err := json.Marshal(struct {
		Raw string `json:"raw"`
	}{base64.RawURLEncoding.EncodeToString(raw)})
	if err != nil {
		return SendResponse{Message: "Cannot encode outgoing message."}
	}
	token, err := a.Tokens.Token(ctx)
	if err != nil {
		return SendResponse{Retryable: true, Message: "Gmail sign-in required."}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Base+"/messages/send", bytes.NewReader(body))
	if err != nil {
		return SendResponse{Message: "Invalid Gmail send endpoint."}
	}
	// A POST with a replayable body must not be replayed by redirects or by a
	// custom idempotency header. No GET-style retry loop is used here.
	req.GetBody = nil
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := *a.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return SendResponse{Unknown: true, Message: "Gmail send outcome is unknown; check Sent before sending again."}
	}
	defer resp.Body.Close()
	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, defaultResponseLimit+1))
	if resp.StatusCode == 429 {
		return SendResponse{Retryable: true, Message: "Gmail rate limit; retry is scheduled."}
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 408 {
		return SendResponse{Message: "Gmail rejected sending; check recipients, account access and authorization."}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || readErr != nil || len(payload) > defaultResponseLimit {
		return SendResponse{Unknown: true, Message: "Gmail send outcome is unknown; check Sent before sending again."}
	}
	var receipt struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(payload, &receipt) != nil || receipt.ID == "" || len(receipt.ID) > 512 || strings.ContainsAny(receipt.ID, "\r\n") {
		return SendResponse{Unknown: true, Message: "Gmail response did not confirm sending; check Sent before sending again."}
	}
	return SendResponse{RemoteID: receipt.ID}
}
func (a *API) FindSent(ctx context.Context, messageID string) (ListPage, error) {
	var result ListPage
	query := url.Values{"q": {"rfc822msgid:" + messageID}, "labelIds": {"SENT"}, "maxResults": {"10"}}
	err := a.request(ctx, "GET", "/messages", query, nil, &result)
	return result, err
}
