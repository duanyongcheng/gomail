# M1 Gmail local account settings validation

Validated 2026-10-29. Gmail and OAuth traffic was limited to local fixtures; no
real Google consent, mailbox or recipient delivery was used.

## Automated gate

`bun run check` passed all configured checks:

- CGO-free Go tests, Go vet and internal race tests
- MyGO vet and generated bindings
- TypeScript type check and Vitest
- Frontend production build
- darwin/amd64, linux/amd64+arm64, windows/amd64+arm64 cross-compilation

Focused regression coverage verifies atomic storage rollback, preservation of
credentials/order/provider identity/sync JSON, email reassignment rejection,
account cascade/isolation, v28→v29 migration and unchanged Rust v20 schema;
encrypted signature round-trip/tamper/AAD/missing-key behavior; legacy migration
winner and explicit clear; settings values in a new MIME snapshot; and no mutation
of queued MIME after later preference changes. UI coverage exercises local Gmail
save without proxy calls, signature load retry, duplicate submit, failure retention
and generated command routing.

## Packaged macOS runtime

- `bun run build -skip-dmg -skip-notarize` passed and produced the arm64 app.
- `codesign --verify --deep --strict` passed.
- All 25/25 packaged desktop selftest checks passed, including actual settings UI
  edits for sender name, account label, color and encrypted signature, followed by
  same-mailbox reconnection and existing outbox/draft preservation checks.
- Normal empty-profile startup remained running until SIGTERM. The resulting
  database reported `user_version=29` and zero accounts.

Selftest profile: `/tmp/pebble-go-account-settings-profile.w1tOGr`.
Selftest results/screenshots: `/tmp/pebble-go-account-settings-results.4Itfyh`.
Empty profile: `/tmp/pebble-go-empty-v29-profile.vRKEwj`.
Full gate artifacts: `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-VU1AVK`.

## Review fixes revalidation

The three reproduced review findings are fixed: asynchronous signature loading
cannot overwrite an editable body, account switches replace only the automatic
signature, and set/update/migrate authenticate existing ciphertext before writes.
Regression coverage includes wrong-key/tamper/AAD writes, readiness consumers,
StrictMode, stale responses, empty signatures, restored drafts and rich/source
mode switching. Detached initialization-predicate tests were replaced with real
TipTap hook tests. Markdown plain-text sends exclude HTML signature markers.

Final `bun run check` passed: Go tests/vet/race, MyGO vet/bindings, TypeScript,
136 Vitest files / 801 tests, web build and all five cross-compile targets.
Artifacts: `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-PmdcE2`.

Rebuilt and signed the final macOS arm64 package; codesign verification passed.
Packaged selftest passed 25/25, and a fresh empty profile stayed running with
schema29 and zero accounts. Runtime logs, disposable profiles and screenshots:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-review-final-runtime-2mpzy9d6`.

The existing Vite large-chunk advisory remains. Windows/Linux runtime, system
keyring and live Gmail behavior are not covered by this local validation.

## Continuation revalidation

The combined account-settings/cloud-copy continuation passed the full gate again
(136 Vitest files / 804 tests), strict signed-package verification and 26/26
packaged desktop checks, including the existing account-edit and reconnect flows.
The final empty-profile runtime uses v30, with zero accounts. See
[cloud-copy validation](m1-cloud-draft-copies-validation.md) for final evidence.
The original v29 evidence above remains the account-settings-only baseline.
