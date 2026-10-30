package desktop

import (
	"errors"
	"strings"
	"testing"
)

func TestOpenExternalPassesValidWebLinksExactlyOnce(t *testing.T) {
	for _, raw := range []string{
		"https://example.com",
		"http://example.com:8080/path?q=mail%20body&token=a%2Bb#section",
		"HTTPS://EXAMPLE.COM/path",
		"https://example.com./path",
		"http://localhost:3000/",
		"http://127.0.0.1/",
		"https://[2001:db8::1]:8443/path",
		"https://xn--bcher-kva.example/",
		"https://b\u00fccher.example/%E4%B8%AD%E6%96%87?q=%26%3D%3F",
		"https://example.com/?redirect=https%3A%2F%2Fother.example%2F",
	} {
		t.Run(raw, func(t *testing.T) {
			calls := 0
			s := &AppService{OpenURL: func(value string) error {
				calls++
				if value != raw {
					t.Fatal("link was changed before opening")
				}
				return nil
			}}
			if _, err := s.OpenExternal(ExternalURLRequest{URL: raw}); err != nil || calls != 1 {
				t.Fatalf("open failed: %v; calls=%d", err, calls)
			}
		})
	}
}

func TestOpenExternalRejectsUnsafeOrMalformedLinksBeforeOpening(t *testing.T) {
	for _, raw := range []string{
		"", "/relative", "//example.com/path", "https:example.com", "https:///example.com",
		"javascript:alert(1)", "data:text/html,test", "file:///etc/passwd", "mailto:a@example.com",
		"pebble-attachment://localhost/file", "ftp://example.com/", "-https://example.com",
		" https://example.com", "https://example.com ", "https://example.com/white space",
		"https://example.com/\nsecret", "https://exam\tple.com/", "https://example.com/\x00",
		"https://example.com/\u0085", "https://example.com/\u00a0",
		"https://user:secret@example.com", "https://@example.com", "https://user@example.com",
		`https:\\example.com`, `https://example.com\@other.example`, `https://example.com/a\b`,
		"https://example.com/%00", "https://example.com/?q=%0d%0a", "https://example.com/#%09",
		"https://example.com/%7f", "https://example.com/%C2%85", "https://example.com/%5c",
		"https://example.com/%", "https://example.com/?q=%GG", "https://example.com/#%xy",
		"https://example.com/%FF", "https://example.com/?q=%C0%AF",
		"https://", "https://:80/", "https://example.com:/", "https://example.com:0/",
		"https://example.com:65536/", "https://example.com:abc/", "https://example.com:-1/",
		"https://[example.com]/", "https://[::zzz]/", "https://2001:db8::1/",
		"https://[fe80::1%25en0]/", "https://exa%6dple.com/", "https://example..com/",
		"https://-example.com/", "https://example-.com/", "https://example_com/",
		"https://./", "https://example.com../", "https://" + strings.Repeat("a", 64) + ".com/",
		"https://example.com/" + strings.Repeat("a", 64<<10), "https://example.com/\xff",
	} {
		t.Run(raw, func(t *testing.T) {
			calls := 0
			s := &AppService{OpenURL: func(string) error { calls++; return nil }}
			_, err := s.OpenExternal(ExternalURLRequest{URL: raw})
			if err == nil || calls != 0 {
				t.Fatalf("unsafe link was accepted: err=%v; calls=%d", err, calls)
			}
			if err.Error() != "only valid HTTP or HTTPS links without credentials can be opened" {
				t.Fatal("validation error is not generic")
			}
		})
	}
}

func TestOpenExternalDoesNotLeakLinkOrPlatformError(t *testing.T) {
	const raw = "https://example.com/private?token=secret"
	calls := 0
	s := &AppService{OpenURL: func(string) error {
		calls++
		return errors.New("platform failure while opening " + raw)
	}}
	_, err := s.OpenExternal(ExternalURLRequest{URL: raw})
	if err == nil || err.Error() != "could not open the link in your default browser" || calls != 1 {
		t.Fatalf("unexpected failure: %v; calls=%d", err, calls)
	}
	if _, err := (&AppService{}).OpenExternal(ExternalURLRequest{URL: raw}); err == nil || err.Error() != "external links are unavailable" {
		t.Fatalf("missing opener: %v", err)
	}
}
