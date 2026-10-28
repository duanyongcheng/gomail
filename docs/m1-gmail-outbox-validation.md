# M1 Gmail outbox validation

Validated on 2026-10-28, baseline `1cfd3eb`. No real Google send, mailbox,
production profile or system keyring was used; all send requests target loopback
fixtures with synthetic credentials and mail content.

## Automated gate

Final `bun run check` passed:

| Check | Result |
|---|---|
| CGO-free Go tests | Passed |
| Go vet and internal race tests | Passed |
| MyGO vet and generated bindings | Passed |
| TypeScript | Passed |
| Vitest | 129 files, 750 tests passed |
| Frontend production build | Passed |
| darwin/amd64, linux/amd64+arm64, windows/amd64+arm64 CGO-free builds | Passed |

New coverage:

- Single POST for success, malformed/missing receipt, 400/403/408/429/500;
  redirect rejection, disconnected response, oversized response and pre-submit
  cancellation. Profile and send use the same pinned token.
- Sender mismatch yields zero POST; failed durable-start transition yields zero
  POST. Lost response stays unknown and Check Sent confirms only a unique matching
  raw Message-ID/From/SENT item without submitting another message.
- Encrypted self-contained MIME and small separately encrypted list summary;
  staging cleanup before send does not lose attachment bytes. Stable client key
  returns the existing item; changed content under that key is rejected.
- Concurrent claims, preparing vs in-progress recovery, bounded retry/backoff,
  account isolation, cancel/dismiss, V25→V26 migration and v20 schema oracle.
- Queued message survives DB close/reopen. Repeated passes and restart never
  resend unknown outcomes. Injected sent/unknown settlement failures retry only
  SQLite, keeping the provider send count at one.
- App worker cancellation retains unknown on recovery. Typed send command means
  queue acceptance, and UI announces that rather than delivery.
- Outbox states/actions, failed retry, unknown check without retry/cancel,
  inconclusive check errors, settings account filtering and settings integration.

Race checking initially caught unprotected counters in the newly added HTTP test
fixtures after hijacked connections closed. Counters now use atomics/fixture locks;
final race tests pass. These were test-observation races, not suppressed failures.

## Packaged desktop

- macOS arm64 build and `codesign --verify --deep --strict` passed.
- All **14 packaged selftest checks** passed. The send check opens the saved local
  draft in the actual WebView, enters a recipient and clicks Send, verifies one
  accepted outbox item, and parses the mock's sole received MIME message to check
  subject and attachment. Draft, sync, queue and attachment checks still pass.
- Inspected `outbox-sent.png`: Settings → Remote Writes shows one Gmail-accepted
  item with attempts=1, an account filter and the explicit queued-is-not-sent
  notice; the queue acceptance toast does not claim delivery.
- Normal empty profile stayed alive, created v26 with zero accounts/outbox items,
  and exited on SIGTERM with status 0.

Real Gmail delivery, OAuth scope/consent behavior on a live account, aliases and
Windows/Linux runtime remain manual verification. Acceptance is not proof of
recipient delivery. Existing Vite large-chunk warning remains nonfatal.

## Evidence

- Gate: `/tmp/pebble-go-gmail-outbox-check-final.log`.
- Detailed gate artifacts:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-9BrHCf/`.
- Package: `/tmp/pebble-go-gmail-outbox-package-final.log`.
- Final selftest report, screenshots and disposable profiles:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-gmail-outbox-final-tbt1cd_z/`.
- Bundle: `build/darwin-arm64/Pebble Go.app`.

Evidence is machine-local. See [contracts and remaining boundaries](m1-gmail-outbox.md).
