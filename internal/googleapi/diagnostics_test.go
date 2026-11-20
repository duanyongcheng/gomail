package googleapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
)

type failedBody struct{}

func (failedBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failedBody) Close() error             { return nil }

func TestRequestDiagnosticsNeverLogPrivateRequestOrResponse(t *testing.T) {
	for _, stage := range []string{"request", "response_body", "http_status"} {
		t.Run(stage, func(t *testing.T) {
			var log bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&log, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			api := &API{Base: "https://private-host.invalid", Tokens: testToken{}, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
				if stage == "request" {
					return nil, errors.New("private transport diagnostic")
				}
				if stage == "response_body" {
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: failedBody{}}, nil
				}
				return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"private":"server detail"}`))}, nil
			})}}
			err := api.request(t.Context(), http.MethodGet, "/messages/private-id", url.Values{"pageToken": {"private-token"}}, nil, nil)
			if err == nil {
				t.Fatal("failure accepted")
			}
			got := log.String()
			if strings.Contains(got, "private") || !strings.Contains(got, `"operation":"message"`) || !strings.Contains(got, `"stage":"`+stage+`"`) {
				t.Fatalf("diagnostic must contain fixed categories only: %s", got)
			}
		})
	}
}

func TestRequestDiagnosticErrorKinds(t *testing.T) {
	for _, test := range []struct {
		err  error
		kind string
	}{
		{context.Canceled, "cancelled"},
		{context.DeadlineExceeded, "timeout"},
		{&url.Error{Op: "Get", URL: "private", Err: context.DeadlineExceeded}, "timeout"},
		{io.ErrUnexpectedEOF, "unexpected_eof"},
		{io.EOF, "eof"},
		{errors.New("private error"), "transport"},
	} {
		if got := requestErrorKind(test.err); got != test.kind {
			t.Fatalf("got %s, want %s", got, test.kind)
		}
	}
}

func TestRetryDiagnosticsNeverLogPrivateFields(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var log bytes.Buffer
		previous := slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&log, nil)))
		t.Cleanup(func() { slog.SetDefault(previous) })
		calls := 0
		api := &API{Base: "https://private-host.invalid", Tokens: testToken{}, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{StatusCode: 403, Header: http.Header{"Retry-After": {"private-header"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"private message","errors":[{"reason":"private-reason"},{"reason":"rateLimitExceeded"}]}}`))}, nil
			}
			return retryResponse(200, "", ""), nil
		})}}
		if err := api.request(t.Context(), "GET", "/messages/private-id", url.Values{"pageToken": {"private-token"}}, nil, nil); err != nil {
			t.Fatal(err)
		}
		got := log.String()
		if strings.Contains(got, "private") || !strings.Contains(got, `"reason":"rateLimitExceeded"`) || !strings.Contains(got, `"retry":1`) || !strings.Contains(got, `"operation":"message"`) {
			t.Fatal("retry diagnostics did not retain only safe categories", got)
		}
	})
}

func TestHTTPDiagnosticsClassifyBadRequestsWithoutResponseText(t *testing.T) {
	for _, test := range []struct {
		body, reason, status, kind string
	}{
		{`{"error":{"message":"private diagnostic","errors":[{"reason":"failedPrecondition"}],"status":"FAILED_PRECONDITION"}}`, "failedPrecondition", "FAILED_PRECONDITION", "json"},
		{`{"error":{"message":"private diagnostic","errors":[{"reason":"badRequest"}],"status":"INVALID_ARGUMENT"}}`, "badRequest", "INVALID_ARGUMENT", "json"},
		{`{"error":{"errors":[{"reason":"private reason"}],"status":"private status"}}`, "other", "other", "json"},
		{"<html>private proxy response</html>", "other", "other", "non_json"},
		{"", "other", "other", "empty"},
	} {
		remote := decodeHTTPError(400, []byte(test.body))
		if requestReason(remote) != test.reason || requestGoogleStatus(remote) != test.status || remote.responseKind != test.kind || strings.Contains(remote.Error(), "private") {
			t.Fatal("unsafe or incomplete HTTP classification", remote.Error(), requestReason(remote), requestGoogleStatus(remote), remote.responseKind)
		}
	}
}

// Gmail reports the same 400 under two shapes. Both must classify, because the
// live diagnostic could only say reason=other for the second one.
func TestHTTPDiagnosticsClassifyBothGmailErrorShapes(t *testing.T) {
	for _, test := range []struct {
		name, body, reason, status string
	}{
		{
			"legacy errors[]",
			`{"error":{"code":400,"message":"Invalid id value","errors":[{"message":"Invalid id value","domain":"global","reason":"invalidArgument"}]}}`,
			"invalidArgument", "other",
		},
		{
			"google status",
			`{"error":{"code":400,"message":"Request had invalid authentication credentials.","status":"INVALID_ARGUMENT"}}`,
			"other", "INVALID_ARGUMENT",
		},
		{
			"both shapes",
			`{"error":{"code":400,"message":"Invalid id value","status":"FAILED_PRECONDITION","errors":[{"reason":"failedPrecondition"}]}}`,
			"failedPrecondition", "FAILED_PRECONDITION",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			remote := decodeHTTPError(400, []byte(test.body))
			if got := requestReason(remote); got != test.reason {
				t.Fatal("reason", got, "want", test.reason)
			}
			if got := requestGoogleStatus(remote); got != test.status {
				t.Fatal("google status", got, "want", test.status)
			}
			if strings.Contains(remote.Error(), "Invalid") || strings.Contains(remote.Error(), "credentials") {
				t.Fatal("remote message leaked into the error", remote.Error())
			}
		})
	}
}
