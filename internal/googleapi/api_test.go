package googleapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type testToken struct{}

func (testToken) Token(context.Context) (string, error) { return "test", nil }

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
	reads  int
}

func (b *trackedBody) Read(p []byte) (int, error) { b.reads++; return b.Reader.Read(p) }
func (b *trackedBody) Close() error               { b.closed = true; return nil }

func TestResponseLimitsCloseBodyAndPreserveHTTPStatus(t *testing.T) {
	for _, test := range []struct {
		name        string
		length      int64
		status      int
		wantContent bool
		wantReads   bool
	}{
		{"declared oversized", 1 << 30, 200, true, false},
		{"streamed oversized", -1, 200, true, true},
		{"oversized auth error", 1 << 30, 401, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &trackedBody{Reader: strings.NewReader(strings.Repeat("x", 100))}
			api := &API{Base: "http://local.invalid", Tokens: testToken{}, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, ContentLength: test.length, Header: make(http.Header), Body: body}, nil
			})}}
			err := api.requestWithLimit(t.Context(), "GET", "/messages/m", nil, nil, nil, 32)
			var content *ContentError
			if test.wantContent {
				if !errors.As(err, &content) || content.Kind != "too_large" {
					t.Fatal("missing bounded content error", err)
				}
			} else {
				var remote *HTTPError
				if !errors.As(err, &remote) || remote.Status != 401 {
					t.Fatal("HTTP status hidden by content limit", err)
				}
			}
			if !body.closed || (body.reads > 0) != test.wantReads {
				t.Fatal("response body not bounded/closed", body.reads, body.closed)
			}
		})
	}
}

func TestMessageIdentityAndJSONFailuresAreTypedAndDoNotIncludePayload(t *testing.T) {
	for _, payload := range []string{`{"id":"another","raw":"abc"}`, `private invalid payload`} {
		api := &API{Base: "http://local.invalid", Tokens: testToken{}, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
		})}}
		_, err := api.Message(t.Context(), "m")
		var content *ContentError
		if !errors.As(err, &content) || content.Kind != "invalid_content" || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe/untyped content error", err)
		}
	}
}
