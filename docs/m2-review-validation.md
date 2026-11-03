# Independent M1/M2 review

Reviewed November 2, 2026 against the full parent and child PRDs. This review
preserves the original M2 scope. Implementation, synthetic fixtures, native UI
execution and live acceptance are separate evidence categories.

## Corrected defects

| Files | Finding and correction |
| --- | --- |
| `internal/app/protocol_accounts.go`, `ProtocolAccountForm.tsx` | Saved Microsoft connection tests now use the installed provider and its persist-before-use refresh vault. Edited password accounts retain stored passwords server-side, with revision checks. Microsoft explicitly tests saved settings. |
| `internal/app/account_network.go`, `protocol_compat.go`, `src/lib/platform/commands.ts`, command generator/manifest/parity | Thirteen proxy, selected-folder and XOAUTH2 status commands now call real typed services instead of optimistic startup values or action stubs. Protocol edits use encrypted CAS/generation fencing. Empty IMAP folder selection reports the actual INBOX default. |
| `internal/app/services.go`, `account_access.go`, `internal/store/gmail_account_network.go` | Gmail authorization accepts a pinned explicit route. Initial route and encrypted account credentials persist atomically without overwriting another account; failed custom proxies never fall back to direct. Existing-account login cannot silently replace its route. |
| `internal/app/protocol_accounts.go` | Global proxy replacement preserves existing protocol authentication pauses. |
| `internal/provider/graph/api.go` | Graph profile verification uses the same validated mail/UPN policy as Microsoft authorization; an authorized UPN-only mailbox can test and send. |
| `internal/provider/graph/provider.go` | Actual replay HTTP 401/403 becomes a typed resource pause instead of consuming operation retries. Ambiguous server failures remain unknown. |
| `internal/store/provider_state.go` | Folder-scoped tombstones/reset reconciliation reapply active intent and successful receipt overlays without retiring those receipts. |
| `internal/provider/smtp/smtp.go` | Definitive AUTH 535 pauses for reauthorization; 534/538 requires configuration attention. Transient 454 remains retryable. Rejection reaches no send barrier, RCPT or DATA. |
| `internal/provider/pop3/pop3.go` | Historical UIDL mappings are paged by 1,000; memory retains only the bounded current listing and current intersection, rather than the entire historical set. |
| `internal/app/scheduler.go`, `services.go` | Status names the actual Gmail/IMAP/POP3/Outlook provider and failures use generic mail text. |
| `AccountsTab.tsx`, English/Chinese locales | Gmail edit guidance points to the implemented connection-route panel instead of claiming proxy settings are unavailable. |
| `selftest_m2.go`, `selftest_m2_ui.go`, `selftest.go` | Added seven independent real WebView assertions using the supplied window/runtime, distinct from the original seven backend assertions. |
| `internal/app/scheduler_idle_test.go` | Removed a scheduling-dependent fixture assumption: block the first sync, emit 19 further IDLE notifications while it is active, then assert exactly one flight and no follow-up. The production scheduler is unchanged. Race stress passed 100 repetitions. |
| `tests/features/settings/CloudSyncTab.test.tsx` | Existing tests edited inputs before asynchronous stored configuration loaded. Wait for the loaded Save control before editing; no M5 application behavior was changed. |

Regression tests accompany these corrections in the app compatibility tests,
Graph API/provider tests, store protocol mutation tests, SMTP/POP3 fixture tests,
scheduler tests, frontend compatibility/form tests and desktop selftest.

## Compatibility inventory

The regenerated manifest contains 145 commands with 71 implemented adapters.
All prior M2 proxy/folder/status startup stubs and action stubs are bridged except
`set_xoauth2_refresh`. That raw-token import is explicitly rejected with guidance
to Microsoft sign-in: the legacy behavior treated unverified token material as
an IMAP password and could not establish mailbox identity or resource scopes.
Safe Microsoft PKCE authorization, independent protocol policies, grant refresh
and status remain implemented through the reachable Microsoft settings UI.

The legacy `complete_oauth_flow` remains Gmail-specific. Its old arguments cannot
express Microsoft's client/tenant/audience/resource policy. Current Microsoft UI
uses `MicrosoftService` directly, while Gmail's legacy proxy arguments now work.
Neither obsolete path is silently reported as implemented Microsoft support.

## Native synthetic coverage

The original seven `M2` provider assertions construct a separate temporary Go
runtime. They prove backend behavior in the packaged process; they are not UI
tests. The new seven assertions explicitly contain `WebView` in their names and
drive DOM inputs/buttons through the real typed bindings:

1. Enter IMAP/SMTP endpoints, explicit fixture CA trust and synthetic passwords;
   test the entered settings through the form.
2. Add the account and assert encrypted credential storage in the UI runtime.
3. Edit the account and save only Projects as its selected folder.
4. Select the account and click manual sync; assert only Projects is cached and
   remains unread before opening.
5. Open the message; assert rendered body, SQLite read flag, server unread count
   and drained durable pending operations.
6. Compose from that account with To/Bcc and HTML body, then click Send; assert
   one accepted Outbox result without a remote ID.
7. Assert two SMTP envelope recipients and no Bcc header/address in transmitted
   MIME.

Injected JavaScript does not call services directly. DB reads and fixture server
observations assert effects. These native checks cover IMAP+SMTP; POP3/Microsoft
native forms are not covered by this scenario. Synthetic unit/component tests
cover additional provider and UI paths. SMTP acceptance is not delivery.

## Isolation and outstanding acceptance

This review uses new temporary profiles, loopback servers and an in-memory
keyring. No normal application launch, real account/profile/keyring access,
commit, push, dependency upgrade or task bookkeeping is performed. The Rust
v20 oracle, pinned dependencies and previous MIME-name fix are retained.
The independently reviewed network API is unchanged, including explicit CA
input limits of 1 MiB PEM and 128 certificates per endpoint. Account settings
reuse that validation instead of introducing a weaker trust bypass.

Installed Bun 1.2.16 ignores `--no-env-file`: an isolated synthetic `.env`
sentinel demonstrated this. `--env-file=/dev/null` suppressed that sentinel and
was used for this review's Bun checks/builds, including child processes. No
installation step was needed. Earlier coordinator isolation claims based on
`--no-env-file` are historical caveats, not proof of isolation. No optional
shell annotation tool or machine-local wrapper is required to use the app.

Live Gmail refresh/revocation/reconnect, OS keyring behavior, actual Microsoft
personal/organizational consent and SMTP AUTH policy, recipient delivery and
platform runtime acceptance remain pending user-controlled dedicated accounts.
No synthetic result marks these rows passed.

IMAP IDLE watches the first selected folder; others poll. Sparse high UID ranges
can require many bounded backfill passes. POP3 does not implement remote flags,
folders or deletion. Graph remains experimental with a 3 MiB raw MIME send limit;
large-attachment upload sessions are not advertised. Unknown sends/moves stay
quarantined and cannot authorize automatic resend or provider fallback.

## Executed native evidence

The ad-hoc signed arm64 package is `build/darwin-arm64/Pebble Go.app`.
`codesign --verify --deep --strict` passed. A complete run with a new disposable
profile exited normally and its JSON reports **89/89 true assertions**, including
all seven backend M2 assertions and all seven real WebView M2 assertions:

- `/private/tmp/pebble-review-native-ak456z9d/results/runtime.json`
- `/private/tmp/pebble-review-native-ak456z9d/results/m2-ui-stages.json`
- `/private/tmp/pebble-review-native-ak456z9d/results/m2-ui-stage.png`

Two earlier native attempts passed account test/add but stopped at a faulty
test-script edit locator. The first matched the sidebar and opened Gmail; the
second matched the duplicate display-name text. The final locator uses the
account row's stable account ID. Those failed attempts were not counted as UI
acceptance. No application functionality was inferred from a zero process exit
without checking every JSON assertion.

The package contains all production corrections. The subsequent IDLE fixture
synchronization change affects `_test.go` only and does not change that binary.
## Final quality gate

Final `bun run check` passed on the reviewed tree, with logs at
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-NskKUm`:

- All Go tests and vet; all internal race tests; MyGO vet.
- Generated bindings and TypeScript.
- Vitest: 175 files, 1,230 tests passed.
- Vite build and five cross-compiles: darwin/amd64, linux/amd64+arm64,
  windows/amd64+arm64. These are compile evidence, not platform runtime tests.
- `git diff --check` and unchanged dependency/oracle checks passed.

An earlier final-gate attempt was blocked by loopback bind permissions. A retry
exposed the IDLE fixture scheduling assumption; another exposed the asynchronous
CloudSync test setup. Both test defects were corrected at their synchronization
boundaries, without weakening assertions or changing unrelated feature scope.
The final complete gate above passed. All delegated reviewers finished before
handoff. Manual acceptance rows remain unchanged.

## Main-session independent final verification

After all implementation/review workers exited, the main session reran the full
`bun run check` on the final tree. Every stage passed, including **175 Vitest files
/ 1230 tests**. Logs: `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-e8mbkT`.

The user-facing package was then rebuilt with the existing OAuth-safe
`bun run build:local` wrapper, preserving locally configured packaged OAuth
defaults instead of the reviewers' explicitly blank fixture build values. Bun
loaded the ignored local environment configuration as designed; no values were
printed or manually opened. Frozen install, binding generation, TypeScript/Vite,
Go compilation and strict deep ad-hoc signature verification passed. Bundle:
`build/darwin-arm64/Pebble Go.app` (21.1 MB).

That exact final bundle was run with a fresh synthetic profile and in-memory
keyring: **89/89 passed**, including **7/7 M2 WebView assertions**, exit0. Artifacts:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-m2-final-runtime-k9g7fyxd`.
No real account/keyring was accessed by that run.

The main session checked for an existing normal app before a disposable normal
empty-profile smoke. None was running; the fresh empty-profile app stayed alive
for5seconds and exited0 on SIGTERM. Artifacts:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-m2-empty-smoke-24sstlzj`.
No normal user profile was opened. Dependencies and the loopback PoC remain
unchanged; `git diff --check` passed. Real-user acceptance rows remain pending.

## M2 panel localization continuation (2026-11-03)

Account setup and editing, IMAP/POP3/SMTP endpoint settings, Microsoft
authorization, server folder actions, global proxy and Gmail account routing
now use the existing English/Chinese i18n resources. This includes accessible
names, help text, loading/success/static failure states, destructive confirmation
messages and independent Microsoft resource authorization states. Raw state
identifiers such as `reauth_required` are replaced with readable localized labels.
Backend error diagnostics retain their existing language.

Language changes preserve form state, stored revisions, blank password retention
and raw protocol/resource values. Static load/cancel failures are stored as state
flags and connection results as structured data, then translated during rendering;
changing language therefore neither reloads account configuration nor repeats a
connection test. Native form selectors use stable form/control identities instead
of English labels; action/result checks accept both supported languages.

`ProtocolPanels.i18n.test.tsx` uses real i18next instances to verify unsaved-edit
retention and success retranslation, failed-load save blocking, independent
Microsoft status and unchanged authorization requests, Chinese delete/dismiss
confirmations, and Gmail route saving. The locale check verifies matching keys
and interpolation placeholders in the four new resource groups. The focused
six-file suite passed 34 tests without fixture warnings.

The full `bun run check` passed: **177 Vitest files / 1241 tests**, Go tests/vet,
race checks, MyGO vet, generated bindings, TypeScript, Vite and all five
cross-compilation targets. Logs:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-ClIO13`.
These checks used a temporary Bun wrapper passing `--env-file=/dev/null` to child
Bun processes. The local user-facing app was then built with `bun run build:local`,
which preserves the existing configured OAuth build defaults. Frozen install made
no dependency changes, and strict deep ad-hoc signature verification passed.

That exact signed arm64 app ran with Chinese UI, a fresh disposable profile,
synthetic loopback servers and an in-memory keyring: **89/89 checks passed**,
including **7/7 M2 WebView checks**, exit 0. The captured Chinese IMAP settings
form was visually inspected. Artifacts:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-m2-i18n-native-qh9yrpfs/results/`
(`runtime.json`, `m2-ui-stages.json`, `m2-ui-localized-settings.png`).
This is native IMAP/SMTP coverage; Microsoft and POP3 localization is covered
by component tests, not a new native provider acceptance claim.

No normal app process was running before the separate disposable empty-profile
smoke. The app remained alive for five seconds, initialized schema 32 with zero
accounts, and exited 0 on SIGTERM. Summary:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-m2-i18n-empty-h5ges1mt/summary.json`.
An earlier smoke wrapper looked for `*.db` instead of `pebble.sqlite`; this was a
verification-script error, corrected before the passing fresh-profile run.

The updated app is `build/darwin-arm64/Pebble Go.app`. This continuation makes no
commit or push and leaves live mailbox, system-keyring, actual recipient delivery
and non-macOS runtime acceptance open.
