# Gmail daily-use continuation

Requested order: finish local account settings → Gmail cloud drafts → production
IMAP/SMTP (M2). This checkout has no local `.trellis/`; parent task metadata and
Pebble-Go core specs live under `/Users/bary/.trellis/`. Existing `docs/m1-*.md`
contracts and executable tests are also guidance.

## Daily-use hardening completed

An intervening reliability/integration pass now adds WAL/FULL writer durability,
safe typed HTTP(S) links with visible sanitized errors, native close preference
and same-window restore, and shared unavailable-feature guards. Review also fixed
auto-signature blocking of local draft access, stale async draft restoration, and
Search/Starred trash action eligibility. Final gate: 146 Vitest files / 899 tests,
Go/vet/race/MyGO/TS/build/cross-compilation passed; packaged macOS selftest 32/32,
external cancellation and normal empty-profile startup passed. No real mailbox
or keyring was used, and changes remain uncommitted. See
[hardening validation](m1-daily-use-hardening-validation.md).

The cloud-draft lifecycle is now implemented and packaged-selftested. Remaining
M1 acceptance is live Gmail/system-keyring verification, followed by production
IMAP/SMTP (M2). This is an explicit cloud-copy workflow rather than an unbounded
bidirectional synchronizer; see [completion contract](m1-gmail-completion.md).

## Review fixes completed (2026-10-30)

The subsequent code review found linked-send cancellation losing latest local
edits, lossy imported-HTML mode conversion, stale conversation opening and
keyboard/palette flag-cache drift. These are now fixed with store retention,
conservative conversion guards and a shared local flag lifecycle. Full gate:
157 Vitest files / 1088 tests; ad-hoc-signed macOS package selftest 47/47; normal
empty-profile startup/schema31 passed. Live acceptance remains pending, and all
changes are uncommitted. See [review validation](m1-review-fixes-validation.md)
for exact evidence and remaining adjacent limitations.

## Account settings

Existing uncommitted implementation is preserved. Revalidation: Go tests, vet,
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
