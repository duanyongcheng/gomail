# M1 Gmail account access / reconnect validation

Validated on 2026-10-29 against baseline `d475bc0`. All authorization and mail
requests used local fixtures; no real Gmail account, old profile, system keyring,
or recipient delivery was used.

## Automated gate

Final `bun run check` passed:

| Check | Result |
|---|---|
| CGO-free Go tests | Passed |
| Go vet / internal race checks | Passed |
| MyGO vet / generated bindings | Passed |
| TypeScript | Passed |
| Vitest | 134 files, 786 tests passed |
| Frontend production build | Passed |
| darwin/amd64, linux/amd64+arm64, windows/amd64+arm64 compilation | Passed |

Added regression coverage includes:

- Corrupt first-account token does not skip later healthy accounts; encrypted
  bytes remain intact and restore performs no Gmail/browser/keyring writes.
- Locked keyring leaves accounts visible and preserves credentials.
- Permission/revocation state survives restoration; late successes cannot clear a
  terminal pause and old provider generations cannot contact Gmail after reconnect.
- Refresh invalid_grant/client failure vs network/quota/5xx classification;
  explicit insufficient-scope403 vs unrelated403; scope/refresh-token retention.
- Local state persistence failure still pauses access and retries SQLite only.
- Same-mailbox reconnect preserves identity/name/color/label/order/checkpoint,
  encrypted local drafts, unknown outbox payload, and stale vault CAS protection.
- Wrong mailbox, insufficient new grant, denial, cancel, failed save, credential
  conflict and account deletion keep original data or prevent resurrection.
- Sign-in can be cancelled while browser authorization is pending or while waiting
  for old queue work. Other account access remains available during the browser.
- Queue claim exclusions, access-release attempt budget, unknown-release guard,
  another account sending, and eligible queued work resuming once after reconnect.
- Virtual ten-minute scheduling verifies only the blocked account stops polling,
  another account continues, and the recovered account resumes.
- Settings loading/read failure/retry, scope/configuration display, duplicate click,
  cancellation, wrong mailbox, account removal during a call, and event refresh.

The final added preparation-cancellation and token-envelope scope assertions also
passed targeted race checks after the full gate. No application behavior changed
after the packaged build.

## Packaged macOS runtime

- Final macOS arm64 application package and `codesign --verify --deep --strict`
  passed.
- **20/20 packaged desktop selftest checks passed.** Existing reading/thread/labels/
  privacy/download/draft/send checks remain. New checks verify local connection
  status, click **Reconnect** in the real WebView, perform a local PKCE callback
  and token exchange, verify the same mailbox, and retain a draft plus a synthetic
  outcome-unknown outbox entry. Only the existing one synthetic send is submitted;
  the unknown item is not resent.
- Inspected `account-reconnected.png`: Chinese status text, the Reconnect button,
  the explanation of same-mailbox preservation, and success feedback are visible.
- Normal empty profile stayed running, schema v28, zero accounts, then exited
  cleanly on SIGTERM (status 0).

Windows/Linux runtime and live Google consent remain manual verification. The
existing nonfatal Vite large-chunk warning remains.

## Evidence

- Full gate log: `/tmp/pebble-go-reauth-check-final.log`.
- Gate details: `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-4h9p0O/`.
- Package log: `/tmp/pebble-go-reauth-package-final.log`.
- Packaged selftest / screenshots / temporary profiles:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-reauth-desktop-hj_y7uud/`.
- Desktop report: `results/runtime.json`; runner summary: `summary.json`.
- Bundle: `build/darwin-arm64/Pebble Go.app`.

See [behavior and limitations](m1-account-reauth.md). Evidence paths are local to
this machine.
