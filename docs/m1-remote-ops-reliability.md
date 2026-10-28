# M1 remote operations reliability follow-up

This follow-up corrects gaps found in the first remote-ops slice. Sending stays
disabled. The 145 invoke commands and their DTO signatures do not change.

## Local-first mutations

`store.QueueMailMutation` owns the complete read/decide/insert/apply transaction.
It handles flags, archive toggles, restore, trash, moves, batches and mark-all-read.
`MailOpPayload` (`remote_id`, `add_labels`, `remove_labels`) is the single intent
used by the local effect, sync overlay and Gmail replay. Application services no
longer pass optional SQL callbacks for production mutations.

- Archive immediately removes Inbox membership; a second toggle restores it.
- Restore adds Inbox and removes Trash/Spam. Moving to Inbox does the same.
- Trash adds Trash and removes Inbox. Other moves retain existing custom labels
  and add the target while removing Inbox, preserving the existing UI contract.
- Flags update both the message booleans and corresponding label memberships.
- Missing messages are skipped; duplicate batch IDs count once. Ownership errors
  roll back the entire batch, including queue rows and sequence numbers.
- All mutation paths and dismiss notify and wake the worker. Pending events
  invalidate account/folder unread counts as well as mail lists.

## Durable order and migration

V24 adds the Go-only `mail_op_sequence` sidecar:

```sql
sequence INTEGER PRIMARY KEY AUTOINCREMENT
op_id TEXT REFERENCES pending_mail_ops(id) ON DELETE CASCADE
kind TEXT CHECK(kind IN ('queued','succeeded'))
UNIQUE(op_id,kind)
```

The legacy v20 table and schema oracle remain unchanged. New `queued` rows are
inserted with the operation, so same-second actions and retries cannot reorder
it. Existing rows are seeded in legacy rowid order; no migration can reconstruct
an order that old profiles never recorded. Random operation IDs no longer use
a sub-millisecond timestamp suffix.

Only the oldest active operation per account/message is eligible. Backoff,
in-progress, outcome-unknown and exhausted failures block that message's later
operations. Other messages/accounts remain eligible. Dismiss releases successors.
`outcome_unknown` and `remote_succeeded` are explicitly excluded from remote
claims, independently of status, retry timestamp and attempt count.

Startup recovery is atomic. Ordinary interrupted writes become pending;
unconfirmed interrupted sends become unknown; recorded successes become done
without remote replay. Recovery also reapplies active local intents to repair
v23 queue-only folder changes when the profile is offline/manual. If recovery
fails, the worker retries recovery rather than doing remote work.

## Remote result persistence

The worker claims just one operation immediately before its call. After a result,
it retains an in-memory settlement until SQLite accepts it. A database failure
retries the settlement only, not the HTTP operation. Other work pauses on this
persistence failure. Settlements and processing are serialized; there is one
startup recovery owner per Runtime. Ready backlog continues after each bounded
20-operation pass instead of sleeping for 30 seconds.

Successful completion atomically stores `_result=remote_succeeded`, marks done
and records a `succeeded` sequence. Failures update their attempt/backoff/result
in one transaction. Errors exposed to the UI and logs are static; arbitrary
provider errors are not persisted. Account deletion/missing messages end safely.

This is **not** an exactly-once guarantee: a hard crash between remote success
and database commit can still replay an idempotent label change. Future sending
must retain its separate unknown-outcome rules. No send endpoint is enabled here.

## Synchronization overlay

Gmail captures a sequence watermark before remote reads while holding its existing
sync/write gate. Each snapshot carries that watermark. Within the cache
transaction, messages first get remote content, then ordered local intents:

1. Active pending/in-progress/failed intents always overlay matching messages.
2. Successful, not-yet-reconciled intents also overlay stale reads.
3. A message fetched by a pass started after success releases those success
   receipts. Later external edits are authoritative instead of being masked
   forever. Messages not fetched by that pass keep their receipts.

A new queued mutation during sync commits either before the snapshot (and is
reapplied by the overlay) or after it (and applies normally). Cache/checkpoint
failure rolls back receipt release and overlays together. Confirmed remote
message deletion retires queued intents without recreating the message.

## Regression lessons

The original tests proved intermediate states rather than the next behavior:

- The unknown-outcome test inspected `failed`/NULL retry but never ran another
  worker pass. The new test actually attempts the next pass and restart.
- The archive-toggle test deleted Inbox membership itself, hiding the missing
  product code. It now asserts the mutation's direct result without SQL repair.
- The packaged check asserted only queue drain. It now forces an authoritative
  fetch and verifies the final Inbox state after restore→archive.

See [validation](m1-remote-ops-reliability-validation.md). Historical validation
reports describe what their old suites tested, not proof of these invariants.
