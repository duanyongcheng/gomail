# M1 Gmail download recovery

This slice follows `1d29119` and removes the requirement that every message in
a page must decode successfully before other mail can sync. It retains
persistent, visible recovery records for unavailable content.

## Message limits and batching

Gmail RAW messages accept up to 50 MiB of decoded RFC 5322 bytes. The MIME parser
also limits the sum of decoded part bytes to 50 MiB and accepts at most 1,000
parts. Gmail's JSON response allowance includes base64 expansion plus 1 MiB for
metadata. Profile, label and History responses retain a 4 MiB bound. The isolated
IMAP/SMTP prototype retains its 2 MiB default MIME limit.

Pages still request at most 100 IDs. To avoid retaining a whole page of large
messages, the provider commits prefixes at an 8 MiB budget for cached body,
attachments and metadata. One larger message can be committed on its own.
HTTP/base64/MIME decoding has additional bounded memory needs; 8 MiB describes
the retained batch, not the entire process's memory use.

The page checkpoint advances only after all IDs have either committed cached
content, confirmed remote deletion or a durable failure record. A transient
error after a committed prefix retains the incoming page checkpoint. Replay
refreshes that prefix safely. Files and FTS follow the existing snapshot
transaction rules.

## Failure persistence and retry

V23 adds `gmail_message_failures`, keyed by account and remote ID, with:

- A static kind: `invalid_content` or `too_large`.
- Attempt count, next eligible retry time and last update time (Unix seconds).
- Whether recovery may announce new mail, determined by the original sync.

No message payload or remote parser/error text is stored in the failure record.
Failures do not overwrite existing cached messages or attachments. Entries
cascade when the account is deleted.

Only message content failures can be recorded and skipped: malformed JSON or
RAW encoding, missing/inconsistent message content, invalid MIME and size
limits. Network/read failures, authorization errors, quota responses, server
failures, cancellation and database errors stop the pass. They do not become
content failures. Only GET 404 confirms deletion and removes a failure record.

Each pass first retries at most ten eligible failures. Continued content
failures back off by 5, 10, 20, 40, 80, 160 and 320 minutes, capped at six hours.
A normal list or History page does not download an already recorded failure;
the bounded recovery batch owns those retries. Remaining eligible work sets
`SyncResult.limited`, which continues after one second even for an explicit
manual sync. A future eligibility time does not create a timer in manual mode.

Successful content or confirmed 404 removes the failure in the same
transaction as its cache changes. Initial backfill failures remain silent on
recovery. New unread Inbox messages first encountered during ordinary History
sync can announce once on recovery; repeated passes use the cache to suppress
duplicate announcements.

## Account settings and typed services

Settings → Accounts → Mail downloads displays the account, cached subject when
available, failure reason, attempts and next eligibility time. The list supports
account filtering and pages of 20 records. Opaque remote IDs and backend parser
errors are not shown to the user. Load/retry failures have explicit error
states, and retry remains disabled while a request is active.

The new view uses generated `SyncService.downloadFailures` and
`SyncService.retryDownloads` through `src/lib/mailDownloads.ts`. Existing
`api.ts` and the 145-command compatibility manifest keep their interface.

`DownloadFailuresRequest` accepts `accountId` (empty for all accounts), `limit`
(default 20, range 1–100) and `offset` (nonnegative). Results contain `items` and
`total`. Explicit retry accepts `AccountRequest`, marks failures immediately
eligible, then uses the account scheduler; disconnected accounts retain their
records and report sign-in required. A retry shared with a pass that already
finished its recovery check arranges another pass if needed. Stop and exit use
the scheduler's existing cancellation and worker join.

`SyncResult.deferred_count` reports remaining failures. Realtime status carries
the same count so the account card and status bar translate it using the UI's
language while ordinary synchronization can succeed. The settings list refreshes
after completed/error progress events and while open. Retrying also invalidates
message and unread caches.

## Verification

Provider regressions cover a real 9 MiB MIME attachment, committed prefixes
before transient failure, malformed content across database reopen, cached body
and attachment preservation, incremental announcement deduplication, quota/auth/
server errors, cancellation, oversized responses, ten-message retry budgets and
404 recovery. Store tests cover atomic rollback, account isolation, cascade
cleanup, retry delay and v22→v23 migration. MIME/API tests exercise decode
expansion, part limits, Unicode snippets, bounded responses and static errors.
Service tests cover pagination and retries concurrent with a finishing pass.
Frontend tests cover list states, filtering, pagination, retry and progress
refreshes. The packaged selftest opens account settings and retries a repaired
synthetic message through the actual UI and typed bindings.

Messages still exceeding these bounds remain visible for retry; this slice
provides no separate raw-message export or streaming attachment download.
Real-account acceptance and the remaining M1 privacy, ops/outbox, labels and
sending features are separate work.
