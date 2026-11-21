package app

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/store"
)

func trustedRenderFixture(t *testing.T) (*MessageService, *TrustService) {
	t.Helper()
	compose, _ := composeFixture(t)
	r := compose.Runtime
	for _, account := range []string{"a", "b"} {
		cid := "logo"
		m := domain.Message{ID: account + ":trust", AccountID: account, RemoteID: "trust", FromAddress: "Sender@example.invalid", BodyText: "body", BodyHTML: `<script>bad()</script><img onerror="bad()" src="https://images.invalid/photo.png"><img src="https://tracking.invalid/open.gif" width="1" height="1"><img src="cid:logo"><link rel="stylesheet" href="https://styles.invalid/a.css">`}
		if err := r.DB.CommitSnapshot(t.Context(), store.Snapshot{AccountID: account, Messages: []store.CachedMessage{{Message: m, Attachments: []domain.Attachment{{ID: account + ":file", ContentID: &cid, MIMEType: "image/png", Data: []byte("png")}}}}}); err != nil {
			t.Fatal(err)
		}
	}
	return &MessageService{Runtime: r}, &TrustService{Runtime: r}
}
func TestPersistedTrustPolicyMatrix(t *testing.T) {
	m, trust := trustedRenderFixture(t)
	ctx := t.Context()
	tests := []struct {
		level, account, mode string
		images, trackers     bool
	}{
		{"", "a", `"Strict"`, false, false},
		{"", "a", `{"TrustedSender":"Sender@example.invalid"}`, false, false},
		{"images", "a", `"Strict"`, true, false},
		{"images", "a", `{"TrustedSender":"sender@example.invalid"}`, true, false},
		{"all", "a", `"Strict"`, true, true},
		{"all", "a", `"LoadOnce"`, true, true},
		{"all", "a", `{"TrustedSender":"other@example.invalid"}`, false, false},
		{"all", "b", `{"TrustedSender":"Sender@example.invalid"}`, false, false},
		{"all", "b", `"Strict"`, false, false},
		{"images", "a", `"Off"`, true, true},
	}
	for _, test := range tests {
		t.Run(test.level+test.account+test.mode, func(t *testing.T) {
			if _, err := trust.Remove(ctx, SenderRequest{AccountID: "a", Email: "sender@example.invalid"}); err != nil {
				t.Fatal(err)
			}
			if test.level != "" {
				if _, err := trust.Set(ctx, TrustRequest{AccountID: "a", Email: "SENDER@example.invalid", TrustType: test.level}); err != nil {
					t.Fatal(err)
				}
			}
			result, err := m.Rendered(ctx, MessageRequest{MessageID: test.account + ":trust", PrivacyMode: json.RawMessage(test.mode)})
			if err != nil {
				t.Fatal(err)
			}
			html := result.Rendered.HTML
			if strings.Contains(html, "images.invalid") != test.images || strings.Contains(html, "tracking.invalid") != test.trackers {
				t.Fatal("wrong policy", html)
			}
			if strings.Contains(html, "script") || strings.Contains(html, "onerror") || !strings.Contains(html, "pebble-attachment://") {
				t.Fatal("trust bypassed sanitizer", html)
			}
			if test.mode != `"Off"` && strings.Contains(html, "styles.invalid") {
				t.Fatal("trust enabled external stylesheet")
			}
		})
	}
	if _, err := trust.Remove(ctx, SenderRequest{AccountID: "a", Email: "sender@example.invalid"}); err != nil {
		t.Fatal(err)
	}
	result, err := m.Rendered(ctx, MessageRequest{MessageID: "a:trust", PrivacyMode: json.RawMessage(`{"TrustedSender":"sender@example.invalid"}`)})
	if err != nil || strings.Contains(result.Rendered.HTML, "images.invalid") || strings.Contains(result.Rendered.HTML, "tracking.invalid") {
		t.Fatal("revoke did not restore protection", result, err)
	}
}
func TestTrustServicesAndFailedReadDoNotReturnPermissiveHTML(t *testing.T) {
	m, trust := trustedRenderFixture(t)
	ctx := t.Context()
	changes := 0
	trust.Runtime.OnTrustChanged = func(request SenderRequest) {
		changes++
		if request.Email != "sender@example.invalid" {
			t.Error(request)
		}
	}
	if _, err := trust.Set(ctx, TrustRequest{AccountID: "a", Email: "Sender@example.invalid", TrustType: "all"}); err != nil {
		t.Fatal(err)
	}
	if yes, err := trust.IsTrusted(ctx, SenderRequest{AccountID: "a", Email: "SENDER@example.invalid"}); err != nil || !yes {
		t.Fatal(yes, err)
	}
	if yes, err := trust.IsTrusted(ctx, SenderRequest{AccountID: "b", Email: "SENDER@example.invalid"}); err != nil || yes {
		t.Fatal(yes, err)
	}
	if rows, err := trust.List(ctx, AccountRequest{AccountID: "a"}); err != nil || len(rows) != 1 || changes != 1 {
		t.Fatal(rows, err, changes)
	}
	conn, err := sql.Open("sqlite", m.Runtime.DB.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec("DROP TABLE trusted_senders"); err != nil {
		t.Fatal(err)
	}
	result, err := m.Rendered(ctx, MessageRequest{MessageID: "a:trust", PrivacyMode: json.RawMessage(`"LoadOnce"`)})
	if err == nil || result.Rendered.HTML != "" {
		t.Fatal("DB error returned HTML", result, err)
	}
}
