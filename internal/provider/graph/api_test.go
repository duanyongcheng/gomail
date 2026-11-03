package graph

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func fixture(t *testing.T, handler http.HandlerFunc) (*API, *httptest.Server) {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Error("missing authorization")
		}
		if r.Header.Get("Prefer") != `IdType="ImmutableId"` {
			t.Error("missing immutable ID policy")
		}
		handler(w, r)
	}))
	t.Cleanup(s.Close)
	a := NewAPI(s.Client(), func(context.Context) (string, error) { return "fixture-secret", nil })
	a.base = s.URL + "/v1.0/me"
	return a, s
}

func TestProfileCanonicalUPNFallbackMatchesAuthorization(t *testing.T) {
	for _, tc := range []struct{ mail, upn, want string }{
		{"", "user@example.test", "user@example.test"},
		{"primary@example.test", "login@example.test", "primary@example.test"},
		{"invalid mailbox", "user@example.test", ""},
		{"", "Display <user@example.test>", ""},
	} {
		t.Run(tc.mail+tc.upn, func(t *testing.T) {
			a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Query().Get("$select"), "userPrincipalName") {
					t.Error("canonical fallback was not requested")
				}
				json.NewEncoder(w).Encode(map[string]string{"id": "stable", "mail": tc.mail, "userPrincipalName": tc.upn})
			})
			identity, err := a.Profile(t.Context())
			if tc.want == "" {
				if err == nil {
					t.Fatal("invalid profile accepted")
				}
			} else if err != nil || identity.Email != tc.want {
				t.Fatalf("identity=%+v error=%v", identity, err)
			}
		})
	}
}

func TestReadPagesAndPartialRemoval(t *testing.T) {
	var base string
	a, s := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/me":
			io.WriteString(w, `{"id":"user","mail":"user@example.test","displayName":"User"}`)
		case "/v1.0/me/mailFolders":
			json.NewEncoder(w).Encode(map[string]any{"value": []Folder{{ID: "in", DisplayName: "Inbox", ChildFolderCount: 1}}, "@odata.nextLink": base + "/mailFolders?$skiptoken=a%2Bb"})
		case "/v1.0/me/mailFolders/in/childFolders":
			io.WriteString(w, `{"value":[{"id":"child","displayName":"Child"}]}`)
		case "/v1.0/me/mailFolders/inbox":
			io.WriteString(w, `{"id":"in","displayName":"Renamed inbox"}`)
		case "/v1.0/me/mailFolders/in/messages/delta":
			if r.URL.RawQuery == "$deltatoken=opaque%2B%2F" {
				json.NewEncoder(w).Encode(map[string]any{"value": []any{}, "@odata.deltaLink": base + "/mailFolders/in/messages/delta?$deltatoken=stable"})
				return
			}
			fmt.Fprintf(w, `{"value":[{"id":"m","isRead":false,"flag":{"flagStatus":"flagged"}},{"id":"gone","@removed":{"reason":"deleted"}},{"id":"partial"}],"@odata.nextLink":%q}`, base+"/mailFolders/in/messages/delta?$deltatoken=opaque%2B%2F")
		case "/v1.0/me/messages/m/$value":
			io.WriteString(w, "From: user@example.test\r\n\r\nhello")
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	base = s.URL + "/v1.0/me"
	ctx := context.Background()
	identity, err := a.Profile(ctx)
	if err != nil || identity.Email != "user@example.test" {
		t.Fatalf("identity: %+v %v", identity, err)
	}
	folders, err := a.FolderPage(ctx, "", "")
	if err != nil || len(folders.Folders) != 1 || folders.NextLink == "" {
		t.Fatalf("folders: %+v %v", folders, err)
	}
	children, err := a.FolderPage(ctx, "in", "")
	if err != nil || len(children.Folders) != 1 {
		t.Fatalf("children: %+v %v", children, err)
	}
	known, err := a.WellKnownFolder(ctx, "inbox")
	if err != nil || known.ID != "in" {
		t.Fatalf("known: %+v %v", known, err)
	}
	p, err := a.Delta(ctx, "in", "")
	if err != nil || len(p.Messages) != 3 {
		t.Fatalf("delta: %+v %v", p, err)
	}
	if p.Messages[0].IsRead == nil || *p.Messages[0].IsRead || p.Messages[0].Flag.FlagStatus != "flagged" || !p.Messages[1].Removed || p.Messages[2].IsRead != nil {
		t.Fatal("partial/tombstone semantics lost")
	}
	p, err = a.Delta(ctx, "in", p.NextLink)
	if err != nil || p.DeltaLink == "" || len(p.Messages) != 0 {
		t.Fatalf("empty continuation: %+v %v", p, err)
	}
	raw, err := a.Raw(ctx, "m")
	if err != nil || !strings.HasSuffix(string(raw), "hello") {
		t.Fatalf("raw: %v", err)
	}
}

func TestRejectContinuationBeforeToken(t *testing.T) {
	var calls atomic.Int32
	a := NewAPI(nil, func(context.Context) (string, error) { calls.Add(1); return "secret", nil })
	base := "https://graph.microsoft.com/v1.0/me/mailFolders/in/messages/delta"
	for _, link := range []string{
		"http://graph.microsoft.com/v1.0/me/mailFolders/in/messages/delta",
		"https://graph.microsoft.com.evil.test/v1.0/me/mailFolders/in/messages/delta",
		"https://graph.microsoft.com:444/v1.0/me/mailFolders/in/messages/delta",
		"https://attacker@graph.microsoft.com/v1.0/me/mailFolders/in/messages/delta",
		base + "#fragment", base + "/../delta", strings.Replace(base, "/in/", "/%69n/", 1), strings.Replace(base, "/in/", "/other/", 1), strings.Replace(base, "/me/", "/users/u/", 1), base + "%2F", base + "?x=\r\n", strings.Repeat("x", maxLink+1),
	} {
		if _, err := a.Delta(context.Background(), "in", link); err == nil {
			t.Errorf("accepted unsafe link %q", link)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("token requested for rejected continuation")
	}
}

func TestReturnedLinksAndCursorFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		expired    bool
	}{
		{"expired", "", 410, true},
		{"unsafe", `{"value":[],"@odata.nextLink":"https://evil.test/"}`, 200, false},
		{"conflict", `{"value":[],"@odata.nextLink":"x","@odata.deltaLink":"y"}`, 200, false},
		{"missing", `{"value":[]}`, 200, false},
		{"malformed", `{"value":[}`, 200, false},
		{"id missing", `{"value":[{}],"@odata.deltaLink":"x"}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) })
			_, err := a.Delta(context.Background(), "in", "")
			if err == nil || errors.Is(err, ErrCursorExpired) != tc.expired {
				t.Fatalf("error: %v", err)
			}
		})
	}
}

func TestMetadataAndRawBounds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
		raw   bool
		known bool
	}{{"metadata known", metadataLimit, false, true}, {"metadata stream", metadataLimit, false, false}, {"raw known", rawLimit, true, true}, {"raw stream", rawLimit, true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.known {
					w.Header().Set("Content-Length", fmt.Sprint(tc.limit+1))
					w.WriteHeader(200)
					return
				}
				w.(http.Flusher).Flush()
				chunk := strings.Repeat("x", 32<<10)
				for written := 0; written <= tc.limit; written += len(chunk) {
					if _, err := io.WriteString(w, chunk); err != nil {
						return
					}
				}
			})
			var err error
			if tc.raw {
				_, err = a.Raw(context.Background(), "m")
			} else {
				_, err = a.Profile(context.Background())
			}
			if err == nil {
				t.Fatal("accepted oversized response")
			}
		})
	}
}

func TestWriteReceiptsAndSendMIME(t *testing.T) {
	raw := []byte("From: user@example.test\r\nBcc: hidden@example.test\r\n\r\nhello")
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1.0/me/messages/m":
			if r.Method != "PATCH" || !strings.Contains(string(b), `"flagStatus":"flagged"`) {
				t.Errorf("flags %s %s", r.Method, b)
			}
			w.WriteHeader(200)
			io.WriteString(w, `{}`)
		case "/v1.0/me/messages/m/move":
			if string(b) != `{"destinationId":"dest"}` {
				t.Errorf("move payload %s", b)
			}
			w.WriteHeader(201)
			io.WriteString(w, `{"id":"m2"}`)
		case "/v1.0/me/mailFolders":
			w.WriteHeader(201)
			io.WriteString(w, `{"id":"new"}`)
		case "/v1.0/me/mailFolders/new":
			if r.Method == "DELETE" {
				w.WriteHeader(204)
			} else {
				w.WriteHeader(200)
				io.WriteString(w, `{}`)
			}
		case "/v1.0/me/sendMail":
			if r.Header.Get("Content-Type") != "text/plain" {
				t.Error("incorrect MIME content type")
			}
			decoded, err := base64.StdEncoding.DecodeString(string(b))
			if err != nil || string(decoded) != string(raw) {
				t.Error("frozen MIME/Bcc changed")
			}
			w.WriteHeader(202)
		default:
			t.Errorf("unexpected write %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	ctx := context.Background()
	v := true
	if unknown, err := a.PatchFlags(ctx, "m", &v, &v); err != nil || unknown {
		t.Fatalf("flags: %v %v", unknown, err)
	}
	if id, unknown, err := a.Move(ctx, "m", "dest"); err != nil || unknown || id != "m2" {
		t.Fatalf("move: %s %v %v", id, unknown, err)
	}
	if f, unknown, err := a.CreateFolder(ctx, "", "New"); err != nil || unknown || f.ID != "new" {
		t.Fatalf("create: %+v %v %v", f, unknown, err)
	}
	if unknown, err := a.RenameFolder(ctx, "new", "Renamed"); err != nil || unknown {
		t.Fatalf("rename: %v %v", unknown, err)
	}
	if unknown, err := a.DeleteFolder(ctx, "new"); err != nil || unknown {
		t.Fatalf("delete: %v %v", unknown, err)
	}
	if accepted, unknown, retry, err := a.Send(ctx, raw); err != nil || !accepted || unknown || retry {
		t.Fatalf("send: %v %v %v %v", accepted, unknown, retry, err)
	}
}

func TestSendFailureMatrix(t *testing.T) {
	for _, tc := range []struct {
		status         int
		unknown, retry bool
	}{{400, false, false}, {401, false, true}, {403, false, true}, {429, false, true}, {408, true, false}, {500, true, false}, {503, true, false}, {302, true, false}, {200, true, false}, {204, true, false}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			var count atomic.Int32
			a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				w.Header().Set("Location", "https://evil.test/secret")
				w.WriteHeader(tc.status)
				io.WriteString(w, "secret-server-payload")
			})
			accepted, unknown, retry, err := a.Send(context.Background(), []byte("mail"))
			if err == nil || accepted || unknown != tc.unknown || retry != tc.retry || count.Load() != 1 {
				t.Fatalf("classification: %v %v %v %v requests=%d", accepted, unknown, retry, err, count.Load())
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("response body leaked")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWriteTransportLossNoReplay(t *testing.T) {
	var calls atomic.Int32
	a := NewAPI(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.GetBody != nil {
			t.Error("write body replay enabled")
		}
		return nil, errors.New("fixture-secret raw transport")
	})}, func(context.Context) (string, error) { return "token", nil })
	if accepted, unknown, retry, err := a.Send(context.Background(), []byte("mail")); err == nil || accepted || !unknown || retry || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("send: %v %v %v %v", accepted, unknown, retry, err)
	}
	if id, unknown, err := a.Move(context.Background(), "m", "f"); err == nil || id != "" || !unknown {
		t.Fatalf("move: %s %v %v", id, unknown, err)
	}
	if calls.Load() != 2 {
		t.Fatal("transport repeated write")
	}
}

func TestSendLocalFailuresDoNotAttempt(t *testing.T) {
	var calls atomic.Int32
	a := NewAPI(nil, func(context.Context) (string, error) { calls.Add(1); return "", errors.New("secret token failure") })
	if _, unknown, _, err := a.Send(context.Background(), []byte("mail")); err == nil || unknown || strings.Contains(err.Error(), "secret") {
		t.Fatalf("auth: %v %v", unknown, err)
	}
	if _, unknown, _, err := a.Send(context.Background(), make([]byte, sendWireLimit)); err == nil || unknown {
		t.Fatalf("oversize: %v %v", unknown, err)
	}
	if calls.Load() != 1 {
		t.Fatal("oversize submission requested token")
	}
}

func TestFindSentCompleteAndAmbiguous(t *testing.T) {
	var base string
	a, s := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/me/mailFolders/sentitems/messages" {
			t.Errorf("unsafe sent route %s", r.URL.Path)
		}
		if r.URL.Query().Get("$skiptoken") == "next" {
			io.WriteString(w, `{"value":[{"id":"b","internetMessageId":"<m@example.test>","isDraft":false}]}`)
			return
		}
		if !strings.Contains(r.URL.Query().Get("$filter"), "internetMessageId eq") {
			t.Error("missing exact filter")
		}
		fmt.Fprintf(w, `{"value":[{"id":"a","internetMessageId":"<m@example.test>","isDraft":false}],"@odata.nextLink":%q}`, base+"/mailFolders/sentitems/messages?$skiptoken=next")
	})
	base = s.URL + "/v1.0/me"
	m, err := a.FindSent(context.Background(), "<m@example.test>")
	if err != nil || len(m) != 2 {
		t.Fatalf("ambiguity evidence lost: %+v %v", m, err)
	}
}

func TestFindSentNeverReturnsPartialEvidence(t *testing.T) {
	var base string
	a, s := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"value":[{"id":"a","internetMessageId":"<m>"}],"@odata.nextLink":%q}`, base+"/mailFolders/sentitems/messages?$skiptoken=loop")
	})
	base = s.URL + "/v1.0/me"
	if m, err := a.FindSent(context.Background(), "<m>"); err == nil || m != nil {
		t.Fatalf("partial evidence returned: %+v %v", m, err)
	}
}

func TestOpaqueIDCanonicalEscaping(t *testing.T) {
	const id = "a/b+c=%2F"
	var base string
	a, s := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/v1.0/me/mailFolders/a%2Fb+c=%252F/messages/delta" {
			t.Errorf("ID path escaped incorrectly: %s", r.URL.EscapedPath())
		}
		fmt.Fprintf(w, `{"value":[{"id":%q}],"@odata.deltaLink":%q}`, id, base+"/mailFolders/a%2Fb+c=%252F/messages/delta?$deltatoken=opaque")
	})
	base = s.URL + "/v1.0/me"
	p, err := a.Delta(context.Background(), id, "")
	if err != nil || len(p.Messages) != 1 {
		t.Fatalf("initial: %+v %v", p, err)
	}
	if _, err = a.Delta(context.Background(), id, p.DeltaLink); err != nil {
		t.Fatalf("continuation: %v", err)
	}
	if _, err = a.Delta(context.Background(), id, strings.Replace(p.DeltaLink, "%2F", "%2f", 1)); err == nil {
		t.Fatal("accepted noncanonical alias")
	}
}

func TestCancellationSanitized(t *testing.T) {
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Raw(ctx, "m"); err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("cancel: %v", err)
	}
}

func TestMoveMalformedReceiptUnknown(t *testing.T) {
	for _, body := range []string{`{}`, `{broken`, `{"id":".."}`} {
		a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201); io.WriteString(w, body) })
		if _, unknown, err := a.Move(context.Background(), "m", "dest"); err == nil || !unknown {
			t.Fatalf("receipt %q: %v %v", body, unknown, err)
		}
	}
}

func TestSentCheckRejectsMismatchedEvidence(t *testing.T) {
	for _, body := range []string{`{"value":[{"id":"a","internetMessageId":"<other>"}]}`, `{"value":[{"id":"a","internetMessageId":"<m>","isDraft":true}]}`, `{"value":[{"id":"a","internetMessageId":"<m>","@removed":{}}]}`} {
		a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		if m, err := a.FindSent(context.Background(), "<m>"); err == nil || m != nil {
			t.Fatalf("bad match returned %+v %v", m, err)
		}
	}
}

func TestSessionPinsTokenBeforeIdentityAndSend(t *testing.T) {
	var tokenCalls atomic.Int32
	var requests atomic.Int32
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/v1.0/me" {
			io.WriteString(w, `{"id":"user","mail":"user@example.test"}`)
			return
		}
		if r.URL.Path != "/v1.0/me/sendMail" {
			t.Errorf("unexpected route %s", r.URL.Path)
		}
		w.WriteHeader(202)
	})
	a.token = func(context.Context) (string, error) {
		if tokenCalls.Add(1) != 1 {
			return "rotated-token", nil
		}
		return "fixture-secret", nil
	}
	session, err := a.Session(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Profile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if accepted, _, _, err := session.Send(context.Background(), []byte("mail")); err != nil || !accepted {
		t.Fatalf("send %v %v", accepted, err)
	}
	if tokenCalls.Load() != 1 || requests.Load() != 2 {
		t.Fatalf("token changed within session: calls=%d requests=%d", tokenCalls.Load(), requests.Load())
	}
}
