# M1 daily-use hardening validation

Validated locally on 2026-10-29. All Gmail/OAuth operations used synthetic
loopback fixtures and in-memory test keyrings; no real mailbox, recipient, system
keyring or external web destination was used. Builds for this validation explicitly
set Google/Microsoft OAuth values empty rather than packaging local credentials.

## Final automated gate

`bun run check` passed after integration and review fixes:

- CGO-free Go tests and Go vet
- Internal race tests
- MyGO vet and generated bindings
- TypeScript
- Vitest: **146 files / 899 tests**
- Frontend production build
- darwin/amd64, linux/amd64+arm64, windows/amd64+arm64 cross-compilation

`git diff --check` passed. Existing Vite large-chunk advisory remains nonfatal.
The test count reflects replacing unsupported-feature expectations with real
availability regressions, not enabling pending commands to satisfy upstream tests.

Gate log: `/tmp/pebble-hardening-workers/final-gate.log`.
Detailed logs and compile artifacts:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-4f6362`.

## What regressions establish

- Actual WAL/FULL writer and all four read-only connection pragmas, physical
  writer replacement, independent pools and database reopen.
- Statement and commit failures at outbox queue/claim/pre-send and cloud-copy
  create/update cause zero remote writes. Saved encrypted drafts/attachments and
  previous cloud receipts remain intact. Repair permits one explicit write;
  unknown outcomes retain existing nonretry guarantees.
- Native lifecycle preference, startup, original-window identity, minimized
  restore, shutdown and concurrent service calls through serialized dispatch.
- Real generated command adapters for web links, profile-scoped close preferences,
  supported Gmail actions, unavailable features and shortcut/navigation policy.
  Static visible failures do not expose link/OS error payloads.
- Search/Starred and combined-inbox actions consume selected-message trash
  membership; mixed/stale batch selection is guarded after confirmation.
- Real ComposeView/LocalDrafts/TipTap integration with nonempty automatic signature,
  mode/account changes, edited signatures, pending recipients/staging, saved draft
  restoration and save failure. Delayed draft Get cannot replace intervening edits;
  deletion rechecks protection after confirmation.
- Delayed frontend bootstrap does not issue a late Show after an early user close.
  UI-dependent workers are joined after App.Run has drained MyGO dispatch, not
  inside its OnQuit callback.

## Final packaged macOS runtime

`bun run build -skip-dmg -skip-notarize` and
`codesign --verify --deep --strict` passed for macOS arm64.

Final packaged selftest passed **32/32**, including all prior Gmail account edits,
signatures, reconnection, local drafts, cloud-copy upload/check/update, outbox,
privacy/trust, labels, attachments and download recovery, plus:

1. Native close before frontend preference/bootstrap completion stays hidden.
2. Actual message-link click invokes the synthetic OS opener once; simulated
   opening failure appears as a sanitized user-visible error.
3. Unsupported settings are not usable.
4. Close/restore preserves the edited compose subject and attachment.
5. A real second process restores the original page and sole native window.
6. Quit-on-close reaches normal native shutdown and exits successfully.

The app uses a selftest-specific application name/instance lock. The link opener
is replaced only before page creation; it never launches a browser. Screenshots
of restored compose and post-reconnection settings were inspected.

Final report, disposable profile, runtime log and screenshots:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-hardening-final-runtime-1m90g07f`.
Package log: `/tmp/pebble-hardening-workers/final-package.log`.
Bundle: `build/darwin-arm64/Pebble Go.app`.

## Cancellation and normal startup

The same behavior was additionally exercised in four isolated packaged runs,
sending SIGTERM after 0.1, 0.8, 2 and 5 seconds. Each process was running before
the signal and exited with code 0 within 0.1 seconds (12-second timeout).
This covers early setup and UI-driving selftest shutdown without a join/dispatch
hang; it is not a proof of every possible OS scheduling interleaving.

A normal (non-selftest) empty profile stayed running for five seconds until
SIGTERM, exited code 0, and contained schema **v30 / zero accounts**.

Cancellation/empty-start report, logs and disposable profiles:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-hardening-shutdown-ic3sb_wd`.

## Scope / remaining acceptance

No schema migration or dependency upgrade was needed. FULL is a SQLite policy
conditional on filesystem/device flush behavior, not power-loss simulation or
exactly-once remote delivery. Cross-compilation is not Windows/Linux runtime
validation. Live Gmail, real keyring and those platforms remain acceptance work.

The full cloud-draft lifecycle and production IMAP/SMTP remain separate tasks.
All product changes, including pre-existing account-settings/cloud-copy work,
remain **uncommitted**; no commits or pushes were made. Machine-local temporary
artifact paths may not survive cleanup. See [behavior contract](m1-daily-use-hardening.md).
