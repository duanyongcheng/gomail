# M1 persisted sender trust validation

Validated on 2026-10-29, baseline `49dbe53`. No real Gmail account, old profile,
system keyring or remote tracker/image request was used.

## Automated gate

Final `bun run check` passed:

| Check | Result |
|---|---|
| CGO-free Go tests | Passed |
| Go vet and internal race tests | Passed |
| MyGO vet and binding generation | Passed |
| TypeScript | Passed |
| Vitest | 133 files, 778 tests passed |
| Frontend production build | Passed |
| darwin/amd64, linux/amd64+arm64, windows/amd64+arm64 CGO-free compilation | Passed |

Added coverage:

- Exact-address normalization, invalid domains/wildcards/display names/controls,
  trust-level validation, cross-account isolation, database close/reopen and
  account cascade deletion.
- Legacy case variants resolve to images-only when levels conflict; explicit
  replacement coalesces them atomically. Injected insert failure rolls back the
  preceding delete. Legacy schema oracle remains unchanged (no migration).
- Service policy matrix: unbacked or mismatched TrustedSender objects are strict,
  images-only cannot escalate, full trust is account scoped, explicit Off remains
  Off, and revoke restores blocking. Script/event/CID protections stay intact.
- Trust database read failure returns no rendered HTML in protected modes.
- Confirmation/cancel, save failure, busy duplicate guard, switching messages
  during confirmation, and backend re-render only after successful persistence.
- Account selection in privacy settings, images/full trust selection, loading,
  read retry, removal failure and ignoring old account responses.
- Revocation during an initial load or in-flight render rejects stale permissive
  HTML; ordinary readers and thread bubbles respond to trust events. Unrelated
  account events do not re-render an already loaded message.
- Existing command manifest routing now invokes the four typed trust methods.

## Packaged desktop

- Final macOS arm64 build and strict codesign verification passed.
- All **17 packaged selftest checks** passed. The new check adds images-only trust
  through the real Privacy settings UI, verifies the stored row and backend
  Strict→images-only behavior, revokes through the UI, and verifies blocking is
  restored. Synthetic image URLs are inspected only in Go and never sent to the
  WebView; there is no remote resource fetch in this check.
- Inspected `trusted-senders.png`: account selection, level selector, save button
  and the warning that trust does not authenticate sender identity are visible.
- Normal empty profile stayed alive with schema v28, zero accounts/trust rows,
  and exited cleanly on SIGTERM (status 0).

Windows/Linux runtime and real mailbox privacy behavior remain manual verification.
Revocation cannot undo network requests made before it. The existing nonfatal
Vite large-chunk warning remains. No schema version bump was needed.

## Evidence

- Gate: `/tmp/pebble-go-trusted-senders-check-final.log`.
- Detailed gate artifacts:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-vpk8Im/`.
- Package: `/tmp/pebble-go-trusted-senders-package-final.log`.
- Final desktop report, screenshots and disposable profiles:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-trusted-senders-final-gpxu8thx/`.
- Bundle: `build/darwin-arm64/Pebble Go.app`.

Evidence is machine-local. See [policy and limitations](m1-trusted-senders.md).
