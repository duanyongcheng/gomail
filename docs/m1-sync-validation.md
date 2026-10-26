# M1 sync validation

Validated the Gmail backfill and scheduler slice on 2026-10-26, based on M0
commit `0506405`. No live Gmail account was used.

## Automated gate

`bun run check` passed:

- CGO-free Go tests, Go vet and internal race tests.
- MyGO vet and generated TypeScript bindings.
- TypeScript and Vitest: 124 files, 715 tests.
- Vite production build.
- CGO-free builds for darwin/amd64, linux/amd64+arm64 and
  windows/amd64+arm64. These verify compilation, not runtime behavior on those
  platforms.

Sixteen new Go tests cover provider pagination/recovery, transactional store
checkpoints and scheduler behavior. Provider fixtures include a 250-message
scan and 250 new History messages beyond an existing cache; a failed second
page resumes with a new provider or after database reopen. Store injection
verifies that a failed checkpoint update rolls back message/FTS/reconciliation
changes. Scheduler tests use a virtual clock for worker coalescing, cancellation,
backoff, background timing, preference transitions and manual continuation.
Two additional provider regressions passed in normal and race modes after the
full gate: delivery during backfill must be recovered from the original
History baseline, and an expired message-page token must restart safely.

## Packaged desktop

`bun run build -skip-dmg -skip-notarize` and
`codesign --verify --deep --strict` passed for the macOS arm64 app (ad-hoc
signing). A packaged `PEBBLE_SELFTEST=1` run in a disposable profile passed all
four checks: React inbox, stored account, provider/search and opaque cursor.
The captured screenshot was inspected; the Inbox and polling status render.

A separate empty-profile packaged run stayed alive for five seconds, created
schema v22 with zero accounts and exited with code zero on SIGTERM.

## Evidence

- Gate log: `/tmp/pebble-go-m1-sync-check.log`.
- Detailed logs and compile artifacts:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-saYngB/`.
- Package log: `/tmp/pebble-go-m1-sync-package.log`.
- Desktop report, screenshot and disposable profiles:
  `/tmp/pebble-go-m1-desktop-_yu87gfo/`.
- App: `build/darwin-arm64/Pebble Go.app`.

Machine-local temporary evidence can be removed by the OS. Reproducible tests
and contracts are stored in the repository. See [sync contracts](m1-sync.md)
for size/pagination limits and remaining M1 scope. Real-account consent and
long-running mailbox acceptance remain manual checks.
