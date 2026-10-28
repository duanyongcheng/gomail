# M1 durable remote operations / outbox

This slice makes Gmail writes durable. It reuses the legacy v20
`pending_mail_ops` table so the Rust schema oracle stays byte identical. The
[reliability follow-up](m1-remote-ops-reliability.md) adds a v24 Go-only sequence
sidecar and corrects local-effect, retry, FIFO and synchronization gaps in the
original slice.

## Local-first writes

User mutations are committed as one transaction: pending rows plus the local
cache effect (flags, folder membership). A failure rolls both back, so the UI
never shows a change that was not persisted.

Queued operation types:

| Operation | Remote effect |
|---|---|
| `update_flags` | add/remove `UNREAD`, `STARRED` |
| `archive` | remove `INBOX` (toggle restores when already archived) |
| `restore` | add `INBOX`, remove `TRASH`/`SPAM` |
| `delete` | add `TRASH`, remove `INBOX` |
| `move_to_folder` | add the target label, remove `INBOX` |
| `send` | reserved; never auto-retried when outcome is unknown |

`delete_permanent`/`empty_trash` reject: `gmail.modify` cannot permanently
delete mail. Batch archive/delete/flags and account/folder mark-all-read use the
same queue; a batch is inserted atomically.

## State machine and retry

Statuses remain `pending`, `in_progress`, `failed`, `done`. The result marker
(`remote_succeeded` or `outcome_unknown`) lives in `payload_json` under
`_result`, keeping the legacy schema untouched.

- Due rows are claimed transactionally, so two workers cannot replay one row.
- Failures back off 60s, doubling to a one-hour ceiling, up to 8 attempts.
- After the budget the row stays `failed` and visible; it is not silently dropped.
- `outcome_unknown` rows are never claimed automatically. A user can dismiss
  them from Remote Writes.
- At startup, interrupted non-send writes return to `pending`; an interrupted
  `send` without a recorded remote success becomes `outcome_unknown`.
- The worker wakes immediately when work is queued instead of waiting for the
  30-second poll, and stops on the application context.

## Service and UI surface

`PendingOpsService` exposes summary, list and dismiss to the existing
`Remote Writes` settings tab. The tab now shows the real queue, next retry time,
last error and an error state for a failed dismiss. `mail:pending-ops-changed`
is emitted after queue/replay/dismiss changes.

Frontend commands now implemented: `archive_message`, `delete_message`,
`restore_message`, `move_to_folder`, `batch_archive`, `batch_delete`,
`batch_mark_read`, `batch_star`, `mark_account_all_read`,
`mark_folder_all_read`, `get_pending_mail_ops_summary`, `list_pending_mail_ops`,
`dismiss_failed_pending_mail_ops`, `get_messages_batch`, and `empty_trash`
(explicit permanent-delete rejection). `api.ts` and the 145-command manifest keep
their signatures.

## Tests

Store tests cover atomic queue/local rollback, claim/fail/backoff/retry, retry
budget, account isolation, interruption recovery, unknown-outcome protection,
dismiss and legacy upgrade. Provider tests cover replayed flags/archive,
malformed or unsupported payloads, server errors and cancellation. App tests
cover local-first flags, background replay, wake-on-enqueue, cancellation,
batch/all-read scoping, archive toggle and folder ownership. Frontend tests
cover typed command routing and the Remote Writes tab states. The packaged
selftest now also verifies local membership and authoritative sync after
restore/archive replay, not just queue drain.

Sending remains out of scope for this slice: no send command is enabled and the
`outcome_unknown` machinery only protects future outbox work. IMAP/SMTP ops are
M2.
