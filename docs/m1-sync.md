# M1 Gmail sync slice

This is the first M1 implementation slice after M0 (`0506405`). It completes
mailbox pagination and scheduling. The remaining M1 reading and writing
features are listed in README.

## Recovery boundaries

`accounts.sync_state.gmail_checkpoint` contains `phase`, `history_id`,
`page_token`, `page_tokens` and `suppress_notifications`. History IDs are opaque
strings. Existing M0 cursors do not imply complete mailbox coverage: an account
without a checkpoint receives a full scan on its first M1 sync.

The phases are `messages` → `reconcile` → `history` → `complete`. Each message
list page contains at most 100 references. A pass handles at most ten pages;
`SyncResult.limited` means another pass is required. The scheduler continues
that work after one second. An explicit manual sync also continues unfinished
backfill, then returns to manual mode without periodic polling.

Before a scan, v22's `gmail_backfill_pending` records the live cached remote IDs
for that account. References processed during the scan are removed from this
set in the same transaction as their messages, folders, FTS changes and next
checkpoint. Remaining references are fetched in bounded batches: a GET 404
soft-deletes the cached message; a successful GET preserves and updates it.
Absence from a message list alone never deletes cached mail.

The scan captures a baseline History ID before its first page. Once the scan
and reconciliation finish, History catches changes from that baseline,
including changes made while scanning. History pages commit independently;
the cursor advances after the final page. A transient failure retains its
incoming page checkpoint. Earlier committed pages and bounded prefixes remain
usable and safe to replay. Content failures follow the durable recovery rules
linked below. History 404 and expired continuation tokens restart a bounded
scan with a fresh baseline. Repeated page tokens fail with the previous
recovery boundary intact.

No historical mail announcements are emitted during a full scan or its catchup.
For ordinary incremental sync, new unread Inbox messages appear in `mail:new`
only after commit. Updates to cached messages do not announce new mail. Events
from committed pages remain available if a later page in the pass fails.

## Scheduling and frontend contract

`Runtime.Restore` remains local and never requests a token or contacts Gmail.
The frontend sends `set_realtime_preference` after loading profile-local
settings. Only then can background workers start. There is one worker and at
most one active pass per account; concurrent triggers share the active pass.

| Preference/context | Delay |
|---|---|
| Realtime, foreground with activity in the past minute | 10s |
| Realtime, foreground idle | 30s |
| Balanced, foreground | 30s |
| Battery, foreground | 60s |
| Background | at least 120s |
| Failures with default policy | 30s / 60s / 120s / 240s / 300s |
| Successful pass with unfinished pagination | 1s |
| Manual, no explicitly requested unfinished work | no timer |

`start_sync`, `stop_sync`, `trigger_sync` and `set_realtime_preference` call typed
`SyncService` bindings. Manual mode ignores automatic triggers. Focus respects
failure backoff; manual and network-online triggers can bypass it. Window blur
updates context without starting a request. Invalid preference, interval or
trigger values reject. The optional poll interval accepts 1–86400 seconds.

Stop, account deletion, preference changes and application exit cancel active
I/O and wait for the worker before replacing it or closing storage. A caller
cancelling its wait does not cancel a pass shared with other callers.

The desktop broadcasts `mail:sync-progress` (started/progress/completed/error),
`mail:sync-complete`, `mail:new` and `mail:realtime-status`. Progress carries
`account_id`, `phase`, `count`, `status`, `message`. Completion describes one
bounded pass. Error event text is static and excludes mail or credential data.

## Validation and current limits

Local fixtures cover 250-message backfill, failed-page recovery after database
reopen, bounded passes, cancellation, expired History, omitted-but-existing
mail, M0 cache upgrade, paginated incremental sync, notification deduplication,
checkpoint rollback with FTS, account isolation and cascade cleanup. Virtual
clock tests cover polling, coalescing, backoff, cancellation, manual continuation
and preference transitions without depending on real-time sleeps.

The later [download recovery slice](m1-recovery.md) raises Gmail's raw and total
decoded MIME allowance to 50 MiB and records content failures for bounded
retries. A malformed or oversized message no longer blocks the mailbox;
transient errors retain the page checkpoint. Large pages can commit bounded
prefixes before the final page checkpoint. Pagination retains its 10,000-token
safety limit per scan or History traversal. All automated Gmail checks use local
servers; real-account consent, refresh and long-running sync are separate
manual acceptance.
