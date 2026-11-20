# Gmail daily-use continuation

Requested order: finish local account settings → Gmail cloud drafts → production
IMAP/SMTP (M2). This checkout has no local `.trellis/`; parent task metadata and
Pebble-Go core specs live under `/Users/bary/.trellis/`. Existing `docs/m1-*.md`
contracts and executable tests are also guidance.

## Current status (2026-11-02)

The user has verified basic Gmail send/reply and the corrected address-name
display. The display-name fix is recorded in independent commit `d4fce6e`; see
[address-name validation](address-name-decoding-validation.md) for the historical
fixture/package evidence. This bounded user confirmation does not establish
OAuth refresh, reconnect, offline restart, system-keyring or expanded recipient
acceptance. No live account actions were performed for this reconciliation.

M1 implementation has recorded automated evidence, with remaining live rows
tracked in the [dedicated-account acceptance checklist](m1-live-acceptance.md).
M1 live acceptance remains open. Production IMAP/SMTP, POP3 and experimental
Microsoft/Graph (M2) now have implementation and synthetic verification tracked
in [the M2 integration report](m2-integration-validation.md). Dedicated-account
acceptance remains separate and pending. Advanced search belongs
to M3 and remains unavailable; its manifest/parity milestone is corrected only.

The validation counts and commit-state descriptions below are historical
snapshots, not checks rerun or current worktree assertions.

The November 3 live-acceptance continuation is tracked in
[M1/M2 live acceptance](m1-m2-live-acceptance.md), including a new full quality
gate, 89/89 packaged synthetic checks, a dedicated Gmail profile and the M2 live
matrix. The dedicated-profile G1 Gmail login/sync/normal-restart substep now
passes; actual expiry/refresh, offline, recipient, revocation, keyring-fault and
non-macOS runtime tests remain open.

The November 4 continuation records a separate real-account profile's bounded
local-draft recovery under account-only connection blocking. Chinese edits,
attachment names and manual mode survived normal quit/restart; M1-05 remains
pending for browser offline, read-state, queued delivery and attachment-byte
evidence. After unlocking, three restored-route sync attempts advanced the cache
to 1,000 unique messages but all ended in temporary connection failures; full
sync success is not accepted for this run. Cancelling reconnection without a
revoked grant preserved the account and local draft. See the same live-acceptance
report for evidence, pending diagnosis and startup/diagnostic deviations.

A further requested retest at 19:55–20:02 reproduced three more history-sync
failures, including after normal quit/restart: 1,300 unique cached messages,
13 saved pages, phase still messages. Targeted sync/network regressions passed;
the live failure was reproducible and its root cause remained unconfirmed then.

The November 5 investigation confirmed `403 rateLimitExceeded` on Gmail message
GETs. The repair replaces subsecond retries with bounded, cancellable exponential
backoff and honors Retry-After. Focused API/provider tests, the full quality gate
and 89/89 packaged selftest checks pass. With one manual trigger, the original
Gmail account/profile advanced from 1,500 to 7,200 unique messages, recovering
the intervening 403 errors automatically. A new message GET HTTP 400 stopped
backfill at page 72, so full history sync still fails acceptance. A diagnostic
package is ready; Mac unlock is needed to reproduce this error and continue
completion/restart retesting. See the live-acceptance record for evidence.
Other M1/M2 live rows remain separate.

The November 20 acceptance run then completed a full backfill on a dedicated
test account (83 messages, checkpoint `complete`) — the first `complete` observed
since the November 3 G1 substep. Large-mailbox backfill remains `deferred` by user
decision and is tracked as G9: the November 5 HTTP 400 at 7,200 messages is still
unexplained, so completeness for mailboxes of that size is unproven.

The earlier November 20 pass hardened the long-backfill path without closing any
acceptance row. A request that was rate-limited first could
never refresh an expired token, a 2xx body cut mid-JSON was quarantined as an
undecodable message, and an account access block was indistinguishable from bad
mail. All three now fail the pass at the committed prefix and resume, and the
existing retry list is reserved for genuinely undecodable content. The original
HTTP 400 remains unexplained; see the live-acceptance record for the exact scope.

## M2 localization continuation (2026-11-03)

The new protocol account, Microsoft authorization, server folder and network
settings panels now follow the English/Chinese language preference. Switching
language preserves unsaved input and does not reload account configuration or
repeat connection tests. Confirmation messages and resource permission states
are localized; backend diagnostics retain their existing language. See the
[November 3 validation](m2-review-validation.md#m2-panel-localization-continuation-2026-11-03).
Dedicated-account acceptance remains open.

## Daily-use hardening completed

An intervening reliability/integration pass now adds WAL/FULL writer durability,
safe typed HTTP(S) links with visible sanitized errors, native close preference
and same-window restore, and shared unavailable-feature guards. Review also fixed
auto-signature blocking of local draft access, stale async draft restoration, and
Search/Starred trash action eligibility. Final gate: 146 Vitest files / 899 tests,
Go/vet/race/MyGO/TS/build/cross-compilation passed; packaged macOS selftest 32/32,
external cancellation and normal empty-profile startup passed. No real mailbox
or keyring was used. The validation report recorded the changes as uncommitted
at that time. See
[hardening validation](m1-daily-use-hardening-validation.md).

The cloud-draft lifecycle is now implemented and packaged-selftested. Remaining
M1 acceptance is live Gmail/system-keyring verification. M2 implementation and
fixture evidence are tracked separately from manual provider acceptance. This
is an explicit cloud-copy workflow rather than an unbounded
bidirectional synchronizer; see [completion contract](m1-gmail-completion.md).

## Review fixes completed (2026-10-30)

The subsequent code review found linked-send cancellation losing latest local
edits, lossy imported-HTML mode conversion, stale conversation opening and
keyboard/palette flag-cache drift. These are now fixed with store retention,
conservative conversion guards and a shared local flag lifecycle. Full gate:
157 Vitest files / 1088 tests; ad-hoc-signed macOS package selftest 47/47; normal
empty-profile startup/schema31 passed. Live acceptance remained pending, and the
report recorded changes as uncommitted at that time. See [review validation](m1-review-fixes-validation.md)
for exact evidence and remaining adjacent limitations.

## Account settings

That pass preserved the then-uncommitted implementation. Historical revalidation: Go tests, vet,
race, MyGO vet, bindings and TypeScript passed; interrupted gate resumed with
136 Vitest files / 801 tests passing. The later combined cloud-copy gate also
passed all Go/frontend/build/cross-compilation checks. Packaged macOS selftest
passed 26/26, including account editing, reconnection and cloud-copy UI.

## Cloud drafts: first vertical slice

Keep local encrypted autosave and its offline guarantees. Add explicit **Upload
to Gmail** / **Update Gmail copy** on saved local drafts, separate from local save
status. Gmail draft IDs are not message IDs and must never be guessed from a
cached DRAFT message. Preserve Bcc, attachments, reply context and sender identity.
Incomplete recipient input remains valid locally; cloud MIME must reject invalid
headers, but allow a draft with no recipients.

Persist an account/local-draft-scoped write journal before a mutating HTTP call.
A network error, malformed success, process crash or failed local receipt write
must block further writes, not create duplicate drafts. A read-only **Check Gmail
copy** can reconcile a known draft or a unique stable Message-ID match. A missing
or changed remote draft is a conflict, not authorization to recreate/overwrite it.
No automatic network writes on local autosave or startup. Opening the local list
must not contact Google. Credentials, MIME and message contents never enter logs.

The original first-slice limits are retained in the historical cloud-copy
document. The follow-up completion adds remote listing/import, conflict recovery,
confirmed remote deletion, rendered Markdown snapshots and draft-aware
`drafts.send`; see [completion contract](m1-gmail-completion.md).

### Acceptance

- [x] Local persistence remains offline and encrypted.
- [x] Create/update pins verified mailbox credentials and performs one write.
- [x] Durable intent blocks retries across restart and lost receipts.
- [x] Check is read-only; detects observed external replacement/deletion (not atomic remote CAS).
- [x] Account isolation, attachment and sender integrity, request bounds tested.
- [x] UI distinguishes local save, cloud copy, conflict and unknown outcome.
- [x] Full Go/frontend/build gate and documentation reflect actual scope.

## IMAP/SMTP entry criteria

Do not connect the loopback-only `internal/mailcore` PoC directly to production.
Use the domain transport and modernc store, encrypted per-account configuration,
verified TLS/STARTTLS, cancellation/timeouts, folder + UIDVALIDITY + UID identity,
and bounded/resumable reads. SMTP must reuse the durable outbox, separate envelope
recipients from Bcc-free MIME, persist before DATA, and never retry unknown DATA
outcomes. POP3, XOAUTH2, proxy policy and destructive IMAP operations need separate
capabilities; unsupported actions remain explicit, not optimistic UI stubs.
