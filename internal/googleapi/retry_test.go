package googleapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type invalidatingToken struct{ onInvalidate func() }

func (t *invalidatingToken) Token(context.Context) (string, error) { return "test", nil }
func (t *invalidatingToken) Invalidate(string)                     { t.onInvalidate() }

func retryResponse(status int, reason, after string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {after}}, Body: io.NopCloser(strings.NewReader(`{"error":{"errors":[{"reason":"` + reason + `"}]}}`))}
}

func TestGETRecoversAfterGmailRateLimitWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		calls, reports := 0, 0
		api := &API{Base: "https://local.invalid", Tokens: testToken{}, OnResult: func(err error) {
			reports++
			if err != nil {
				t.Error("recovered request reported failure", err)
			}
		}, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
			calls++
			if time.Since(started) < 20*time.Second {
				return retryResponse(403, "rateLimitExceeded", ""), nil
			}
			return retryResponse(200, "", ""), nil
		})}}
		if err := api.request(t.Context(), "GET", "/messages/m", nil, nil, nil); err != nil {
			t.Fatal("temporary quota window interrupted the read", err)
		}
		if calls <= 3 || reports != 1 || time.Since(started) > 2*time.Minute {
			t.Fatal("read did not recover within its bounded retry budget", calls, reports, time.Since(started))
		}
	})
}

func TestGETRetryAfterAndCancellation(t *testing.T) {
	for _, header := range []string{"12", "date", "180"} {
		t.Run(header, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				started := time.Now()
				after := header
				if after == "date" {
					after = started.Add(12 * time.Second).UTC().Format(http.TimeFormat)
				}
				calls := 0
				api := &API{Base: "https://local.invalid", Tokens: testToken{}, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						return retryResponse(429, "", after), nil
					}
					if time.Since(started) < 12*time.Second {
						t.Error("retried before the server's cooldown elapsed")
					}
					return retryResponse(200, "", ""), nil
				})}}
				err := api.request(t.Context(), "GET", "/messages/m", nil, nil, nil)
				if header == "180" {
					var remote *HTTPError
					if !errors.As(err, &remote) || remote.Status != 429 || calls != 1 || time.Since(started) != 0 {
						t.Fatal("oversized cooldown must return the HTTP error without an early retry", calls, err)
					}
				} else if err != nil || calls != 2 {
					t.Fatal("cooldown did not recover", calls, err)
				}
			})
		})
	}
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		api := &API{Base: "https://local.invalid", Tokens: testToken{}, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return retryResponse(403, "rateLimitExceeded", "30"), nil
		})}}
		done := make(chan struct{})
		defer func() { <-done }()
		go func() { time.Sleep(5 * time.Second); cancel(); close(done) }()
		if err := api.request(ctx, "GET", "/messages/m", nil, nil, nil); !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatal("cancellation did not stop cooldown without another request", calls, err)
		}
	})
}

func TestGETPersistentQuotaIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		calls := 0
		api := &API{Base: "https://local.invalid", Tokens: testToken{}, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return retryResponse(403, "rateLimitExceeded", ""), nil
		})}}
		err := api.request(t.Context(), "GET", "/messages/m", nil, nil, nil)
		var remote *HTTPError
		if !errors.As(err, &remote) || remote.Status != 403 || calls != 7 || time.Since(started) < 63*time.Second || time.Since(started) > 2*time.Minute {
			t.Fatal("persistent quota must return the original status within a bounded budget", calls, time.Since(started), err)
		}
	})
}

func TestGETRetriesOtherTransientStatuses(t *testing.T) {
	for _, test := range []struct {
		status int
		reason string
	}{
		{403, "userRateLimitExceeded"}, {403, "backendError"}, {429, ""}, {503, ""},
	} {
		t.Run(http.StatusText(test.status)+test.reason, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				api := &API{Base: "https://local.invalid", Tokens: testToken{}, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
					calls++
					if calls < 4 {
						return retryResponse(test.status, test.reason, ""), nil
					}
					return retryResponse(200, "", ""), nil
				})}}
				if err := api.request(t.Context(), "GET", "/messages/m", nil, nil, nil); err != nil || calls != 4 {
					t.Fatal("transient status did not recover", calls, err)
				}
			})
		})
	}
}

func TestGETRetryClosesResponseAndRechecksAccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		body := &trackedBody{Reader: strings.NewReader(`{"error":{"errors":[{"reason":"rateLimitExceeded"}]}}`)}
		blocked := errors.New("provider replaced")
		calls := 0
		api := &API{Base: "https://local.invalid", Tokens: testToken{}, BeforeRequest: func() error {
			if calls != 0 {
				if !body.closed {
					t.Error("previous response was not closed before cooldown")
				}
				return blocked
			}
			return nil
		}, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 403, Header: make(http.Header), Body: body}, nil
		})}}
		if err := api.request(t.Context(), "GET", "/messages/m", nil, nil, nil); !errors.Is(err, blocked) || calls != 1 {
			t.Fatal("stale provider retried after access was blocked", calls, err)
		}
	})
}

func TestGETRefreshesExpiredTokenAfterAReadRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls, invalidated := 0, 0
		api := &API{Base: "https://local.invalid", Tokens: &invalidatingToken{onInvalidate: func() { invalidated++ }}, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
			calls++
			switch calls {
			case 1:
				return retryResponse(403, "rateLimitExceeded", ""), nil
			case 2:
				return retryResponse(401, "", ""), nil
			}
			return retryResponse(200, "", ""), nil
		})}}
		if err := api.request(t.Context(), "GET", "/messages/m", nil, nil, nil); err != nil || calls != 3 || invalidated != 1 {
			t.Fatal("expired token after a read retry was not refreshed once", calls, invalidated, err)
		}
	})
}

func TestRetryDoesNotReplayWritesOrPermissionFailures(t *testing.T) {
	for _, test := range []struct{ method, reason string }{
		{"POST", "rateLimitExceeded"}, {"PUT", "rateLimitExceeded"}, {"DELETE", "rateLimitExceeded"},
		{"GET", "insufficientPermissions"}, {"GET", "domainPolicy"},
	} {
		t.Run(test.method+test.reason, func(t *testing.T) {
			calls := 0
			api := &API{Base: "https://local.invalid", Tokens: testToken{}, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return retryResponse(403, test.reason, ""), nil
			})}}
			if err := api.request(t.Context(), test.method, "/messages/m", nil, nil, nil); err == nil || calls != 1 {
				t.Fatal("non-retryable operation was replayed", calls, err)
			}
		})
	}
}
