# MIME address display-name validation

## Fix

Some mail clients quote RFC 2047 encoded display names, for example:

```text
From: "=?utf-8?B?5q615rC45oiQ?=" <sender@example.invalid>
```

Go's address parser treats quoted names as literal text. The app previously
copied that encoded text into the message metadata, leading to encoded names in
sender/recipient rows and incorrect avatar initials.

`mailmime.DecodeAddressName` now applies best-effort decoding to parsed
From/To/Cc/Bcc names, using the existing charset reader. The shared cached-message
projection uses the same helper, so previously cached mail displays correctly
without a re-download or profile migration. This includes normal/list/starred and
thread metadata reads. It does not rewrite cached rows or alter mailbox addresses.

Malformed residual encoding, unknown charsets, invalid UTF-8 and decoded control
characters preserve the original name. Existing address syntax validation,
reply identity, outgoing header validation and immutable queued sends remain
unchanged.

## Verification — November 2, 2026

- Pre-fix regression reproduced quoted B/Q, encoded ASCII, adjacent/mixed names,
  GBK and existing-cache failures.
- Incoming/draft address tests cover all four address fields, literal names,
  punctuation and safe fallback cases, while asserting unchanged recipient
  addresses/order and reply/body metadata.
- Cached-reader tests cover detail/list/starred/thread and database reopen;
  stored name/recipient bytes remain unchanged after reads.
- Synthetic fixture tests verify that rewriting folded/case-insensitive address
  headers preserves other headers, multipart bodies and attachments.
- Full `bun run check` passed: Go tests/vet/race, MyGO vet, bindings, TypeScript,
  **170 Vitest files / 1211 tests**, frontend build and all five cross-compilation
  targets.
- `bun run build:local` rebuilt the signed macOS arm64 app; strict codesign
  verification passed.
- Packaged synthetic desktop selftest passed **75/75 checks**, including the new
  **MIME address names and avatar decoded through UI** assertion. The captured
  WebView shows decoded Chinese sender/recipient names and the correct avatar.

Artifacts:

- Quality logs: `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-IptqJo`.
- Desktop logs/report/screenshots: `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-address-names-runtime-jqt1tzrr`.
- Bundle: `build/darwin-arm64/Pebble Go.app`.

The desktop run used a disposable profile, synthetic Gmail fixture and in-memory
keyring. It did not access the user's mailbox/profile, Google or system keyring.
The ordinary empty-profile smoke run was not repeated because a normal user app
was already running; it was left untouched. Windows/Linux runtime acceptance is
not implied by cross-compilation. The existing Vite large-chunk warning remains
non-blocking.

## User verification

Fully quit the previously running app (Cmd+Q, not just closing its window), then
open the rebuilt bundle and revisit the affected reply. Already cached sender and
recipient names should decode without resynchronizing or changing the profile.
