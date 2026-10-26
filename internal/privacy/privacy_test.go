package privacy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStrictSanitizesScriptsTrackersCSSAndExternalImages(t *testing.T) {
	rendered := Render(`<html><head><title>secret</title><style>.x{color:red;background:url(https://evil.invalid)}</style></head><body><script>alert(1)</script><p class="x" onclick="bad()">hello</p><img src="https://tracking.mailchimp.com/open.gif" width="1" height="1"><img src="https://images.invalid/photo.png" alt="photo"></body></html>`, "", "a", "a:m", nil, Strict)
	if strings.Contains(rendered.HTML, "script") || strings.Contains(rendered.HTML, "secret") || strings.Contains(rendered.HTML, "onclick") || strings.Contains(rendered.HTML, "evil.invalid") {
		t.Fatalf("unsafe HTML survived: %s", rendered.HTML)
	}
	if !strings.Contains(rendered.HTML, "hello") || !strings.Contains(rendered.HTML, "blocked-image") {
		t.Fatalf("safe text/image placeholder missing: %s", rendered.HTML)
	}
	if rendered.ImagesBlocked != 1 || len(rendered.TrackersBlocked) != 1 || rendered.TrackersBlocked[0].Type != "pixel" {
		t.Fatalf("wrong privacy counts: %+v", rendered)
	}
}

func TestModesAndTrackers(t *testing.T) {
	html := `<style>.x{color:red}</style><img src="https://tracking.mailchimp.com/open.gif" width="10" height="10"><img src="https://images.invalid/photo.png">`
	load := Render(html, "", "a", "a:m", nil, LoadOnce)
	if !strings.Contains(load.HTML, "images.invalid") || strings.Contains(load.HTML, "mailchimp.com") || !strings.Contains(load.HTML, "color:red") {
		t.Fatalf("LoadOnce policy wrong: %s", load.HTML)
	}
	trusted := Render(html, "", "a", "a:m", nil, Mode("trusted:sender@example.invalid"))
	if !strings.Contains(trusted.HTML, "mailchimp.com") || trusted.ImagesBlocked != 0 || len(trusted.TrackersBlocked) != 0 {
		t.Fatalf("trusted policy wrong: %+v", trusted)
	}
	off := Render(`<link rel="stylesheet" href="https://cdn.invalid/mail.css"><p>body</p>`, "", "a", "a:m", nil, Off)
	if !strings.Contains(off.HTML, "cdn.invalid") {
		t.Fatalf("Off dropped stylesheet: %s", off.HTML)
	}
}

func TestCIDUsesOpaqueAttachmentURLAndUnknownCIDDoesNotLoad(t *testing.T) {
	rendered := Render(`<p><img src="cid:logo@example.invalid"><img src="cid:unknown@example.invalid"></p>`, "", "acct", "acct:message", []Attachment{{ID: "acct:message:attachment:0", ContentID: "<logo@example.invalid>"}}, Strict)
	want := AttachmentURL("acct", "acct:message", "acct:message:attachment:0")
	if !strings.Contains(rendered.HTML, want) || strings.Contains(rendered.HTML, "unknown") {
		t.Fatalf("CID mapping wrong: %s", rendered.HTML)
	}
	account, message, attachment, ok := DecodeAttachmentURL(strings.TrimPrefix(want, "pebble-attachment://localhost"))
	if !ok || account != "acct" || message != "acct:message" || attachment != "acct:message:attachment:0" {
		t.Fatal("opaque URL did not round trip")
	}
}

func TestPlainTextFallbackAndModeParsing(t *testing.T) {
	rendered := Render("", `<b>literal</b> & text`, "a", "a:m", nil, Strict)
	if rendered.HTML != `<pre class="pebble-plain-text-email">&lt;b&gt;literal&lt;/b&gt; &amp; text</pre>` {
		t.Fatalf("plain fallback: %s", rendered.HTML)
	}
	if got := ParseMode(json.RawMessage(`{"TrustedSender":"sender@example.invalid"}`)); got != Mode("trusted:sender@example.invalid") {
		t.Fatal(got)
	}
	if got := ParseMode(json.RawMessage(`"Off"`)); got != Off {
		t.Fatal(got)
	}
}
