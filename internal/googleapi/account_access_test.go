package googleapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

func TestConnectionErrorClassification(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
		want   domain.ConnectionState
	}{
		{401, `{}`, domain.ConnectionReauthRequired},
		{403, `{"error":{"errors":[{"reason":"insufficientPermissions"}]}}`, domain.ConnectionPermissionRequired},
		{403, `{"error":{"details":[{"reason":"ACCESS_TOKEN_SCOPE_INSUFFICIENT"}]}}`, domain.ConnectionPermissionRequired},
		{403, `{"error":{"errors":[{"reason":"rateLimitExceeded"}]}}`, domain.ConnectionTemporaryError},
		{403, `{"error":{"errors":[{"reason":"domainPolicy"}]}}`, ""},
		{403, `{}`, ""}, {404, `{}`, ""}, {429, `{}`, domain.ConnectionTemporaryError}, {503, `{}`, domain.ConnectionTemporaryError},
	} {
		if got := ConnectionFailure(decodeHTTPError(test.status, []byte(test.body))); got != test.want {
			t.Fatalf("status %d: %q != %q", test.status, got, test.want)
		}
	}
	for _, err := range []error{context.Canceled, &ContentError{Kind: "invalid_content"}, errors.New("invalid cache")} {
		if got := ConnectionFailure(err); got != "" {
			t.Fatal("unrelated error changed authorization", got)
		}
	}
}

func TestPinnedSessionReportsAccessFailureWithoutRepeatingSend(t *testing.T) {
	for _, status := range []int{401, 403} {
		calls := 0
		var reported []domain.ConnectionState
		api := &API{Base: "http://local.invalid", Tokens: testToken{}, OnResult: func(err error) { reported = append(reported, ConnectionFailure(err)) }, HTTP: &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"errors":[{"reason":"insufficientPermissions"}]}}`))}, nil
		})}}
		session, err := api.SendingSession(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		result := session.SendRaw(t.Context(), []byte("MIME"))
		if calls != 1 || result.Unknown || result.RemoteID != "" || len(reported) != 1 || !reported[0].Blocked() {
			t.Fatal("send/access classification", calls, result, reported)
		}
	}
}

func TestAccessGuardPreventsOldPinnedSessionRequest(t *testing.T) {
	blocked := false
	calls := 0
	api := &API{Base: "http://local.invalid", Tokens: testToken{}, BeforeRequest: func() error {
		if blocked {
			return &domain.AccessError{State: domain.ConnectionReauthRequired}
		}
		return nil
	}, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected request") })}}
	session, err := api.SendingSession(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	blocked = true
	_, _ = session.Profile(t.Context())
	result := session.SendRaw(t.Context(), []byte("MIME"))
	if calls != 0 || result.Unknown {
		t.Fatal("obsolete credentials reached network", calls, result)
	}
}
