# M1 privacy HTML and CID rendering

This slice replaces the M0 escaped-plain-text renderer with a Go-core privacy
boundary. The frontend's `sanitizeHtml` remains a defensive second pass, but
it is not the security boundary.

## Render flow

```text
messages.body_html_raw + body_text + attachment metadata
  -> MessageService.Rendered(messageId, privacyMode)
  -> persisted account/sender trust resolution
  -> internal/privacy.Render
  -> safe HTML + tracker/image counters
  -> generated binding / useMessageLoader
  -> defensive DOMPurify pass / ShadowDomEmail
```

The backend parses HTML with `golang.org/x/net/html`, extracts body content from
full documents, and renders only an allowlist of email tags and attributes.
Script, iframe, object/embed, form controls, document metadata, event handlers,
dangerous URL schemes and CSS remote-loading constructs are removed. Plain text
without an HTML part is emitted as an escaped `<pre class="pebble-plain-text-email">`.

The request accepts `Strict`, `LoadOnce`, `Off` and the external JSON form
`{"TrustedSender":"sender@example.invalid"}`. The [sender-trust follow-up](m1-trusted-senders.md)
now verifies that an override matches this message and its persisted account-scoped
trust level. Unbacked/mismatched overrides become Strict; images-only records
cannot escalate to full trust. After resolution, the renderer behavior is:

| Mode | Ordinary HTTPS images | Known trackers / pixels | Embedded CSS | External stylesheet |
|---|---|---|---|---|
| Strict | Placeholder + count | Block + report | Drop | Drop |
| LoadOnce | Keep | Block + report | Keep after CSS filtering | Drop |
| TrustedSender | Keep | Keep | Keep after CSS filtering | Drop |
| Off | Keep | Keep | Keep after CSS filtering | Keep HTTPS only |

Tracker domains include the known sender/marketing tracker set ported from the
reference privacy crate. A 1×1 image, or an image with one dimension ≤1, is a
pixel unless the mode explicitly trusts the sender. Tracker counters are keyed
by domain/type so duplicate pixels do not inflate the banner.

## CID attachments

A `cid:` image is never treated as a remote URL. The renderer maps a matching
MIME `Content-ID` to:

```text
pebble-attachment://localhost/<base64url(account-id + NUL + message-id + NUL + attachment-id)>
```

Unknown CIDs are removed. The URL contains an opaque token rather than a local
path. MyGo registers `pebble-attachment` before creating windows. Its handler:

1. Accepts only GET/HEAD.
2. Decodes exactly the account/message/attachment tuple.
3. Queries `attachments JOIN messages` and verifies account, message and live
   attachment identity.
4. Validates the stored path is relative and remains under the profile.
5. Opens only that file, sets the stored MIME type, `Cache-Control: no-store`,
   and serves it inline.

A malformed token, cross-account/message tuple, missing DB row, path traversal,
non-regular file or unknown scheme returns 404. The protocol never accepts a
filesystem path or arbitrary query parameter.

## Typed request

`MessageRequest.privacyMode` is optional JSON (`unknown` in generated TypeScript)
so existing `get_message` and `list_attachments` calls remain compatible. The
existing `get_message_with_html` and `get_rendered_html` calls pass the mode to
`MessageService.Rendered`. When omitted, the backend defaults to `Strict`.

The frontend still maps stored privacy preferences and trust actions to the
same existing API. The backend now owns the resulting HTML and tracker/image
counts, so the PrivacyBanner describes the same policy that produced the body.

## Security limits and validation

No script, CSS URL, data URL, arbitrary protocol, external form, or document
metadata is allowed through the backend renderer. `Off` means the user chose to
permit external images and HTTPS stylesheet links; it does not permit script,
`javascript:`, `data:`, `vbscript:` or arbitrary local resources.

Tests cover XSS and CSS removal, body extraction, all four modes, tracker
classification, unknown/matching CIDs, opaque URL round trips, attachment
authorization/path containment, service-level privacy mode forwarding, typed
command preservation, frontend custom-scheme handling, and the existing
privacy banner/message loader behavior. The packaged selftest also renders a
CID-backed message through the real MyGo application setup; real Gmail remains
manual acceptance.
