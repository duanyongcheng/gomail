# M1 Gmail sending and durable outbox

Gmail sending now uses a local encrypted outbox. A successful `send_email` call
means **durably queued**, not delivered. The composer shows a queue-acceptance
toast. For local-only sends it releases the local draft and staging references;
Outbox owns a full MIME copy including attachment bytes. Cloud-linked sends retain
a protected encrypted local draft and its attachment references until acceptance,
so cancelling a queued/failed send can restore editability without losing the
latest local changes. See [the linked-send lifecycle](m1-gmail-completion.md).

## User flow

Open Compose → **Outbox**, or Settings → **Remote Writes** → **Outbox**. The
settings surface supports account filtering. States distinguish queued, checking
sender, sending, failed, outcome unknown and accepted by Gmail. Acceptance does
not guarantee recipient delivery; normal Gmail sync populates Sent afterward.

- Queued/failed items may be cancelled. For cloud-linked sends, cancellation
  retains the latest local draft and unlocks it, then clears the outbox copy.
  Older linked items whose local draft was already deleted cannot be safely
  cancelled: their last outbox content is retained and an explanation is shown.
  Local-only cancellation still removes the queued content.
- Failed items may be explicitly retried. Preflight/network-access failures and
  HTTP 429 back off automatically, from 60s to 1h, up to eight attempts.
- Unknown outcomes have **Check Sent**, not Retry. The check performs read-only
  Gmail search by stable RFC Message-ID and verifies a unique SENT result's exact
  Message-ID and From. Missing/ambiguous results remain unknown; no resend occurs.
- Sent records and unlinked unknown records may be dismissed. Unknown linked
  sends must first be resolved with Check Sent. Dismiss does not undo remote
  delivery; it removes outbox content while retaining a request-key tombstone.

The UI displays at most 50 entries, active first. Dismiss finished records to
reveal older ones. State-change events and three-second local polling refresh it.

## Safety boundary

1. Validate recipients and size, snapshot backend account email/display name,
   build production MIME with explicit Bcc and reply headers, and encrypt it.
2. Persist outbox with an account-scoped stable request key. A repeated key with
   the same request returns the existing item; different content is rejected.
3. Worker transactionally claims as `preparing`, obtains a token, and GETs Gmail
   profile. The profile email must match the saved sender. No aliases/send-as
   are supported yet. Display name is a local configuration snapshot, not a
   Google-verified name.
4. Pin that same access token for submission. Persist `in_progress` **before**
   POST. If this database write fails, do not submit.
5. Perform exactly one application-level POST; no redirect following or read-API
   retry loop. A confirmed Gmail ID yields `sent`. Network/read errors, malformed
   success, HTTP 408/5xx or redirects yield `outcome_unknown`. Explicit other 4xx
   responses are failures; 429 permits backoff retry.
6. Persist the result. If result persistence fails, keep the settlement in memory
   and retry SQLite only, never the POST in that process.

Startup recovers preparing to queued (no send began), but in_progress becomes
unknown. This is not distributed exactly-once: Gmail Message-ID is not an
idempotency key, and a process crash after Gmail acceptance but before local
receipt can only be resolved conservatively. Dismissed/cancelled request keys
cannot be reused to silently create another send.

## Storage and integration

V26 adds a separate `outbox` table. `pending_mail_ops` requires a cached remote
message and handles idempotent label changes; sends must not be forced through
that path. The legacy v20 oracle remains unchanged.

- `data`: encrypted MIME, sender, subject, stable Message-ID and request fingerprint.
- `summary`: separately encrypted subject so list refreshes do not read/decrypt
  large MIME bodies.
- AAD: purpose `outbox.data` / `outbox.summary`, record `accountID NUL outboxID`.
- Metadata (account ID, timestamps, state, retry count, remote ID) remains plaintext.
- Retained encrypted payload+summary quota is 200 MiB. Finished content remains
  until dismissal; account deletion cascades outbox records.
- Existing MIME/compose size bounds apply. Only opaque compose attachment refs
  are accepted; no arbitrary filesystem path is read.

The app worker uses application cancellation and joins before storage closes.
Restore itself remains network-free. Already queued user-authorized sends may run
after startup regardless of manual **mail-sync** preference; manual sync is not
an outbox pause switch. Preparing/in-progress cannot be cancelled from the UI.

`domain.SendTransport` separates provider I/O from store transitions. Generated
`OutboxService.queue/list/action` bindings serve the UI; the legacy send command
keeps its Promise<void> signature with an optional clientKey. The inventory stays
145 commands, 43 implemented.

## Deferred work

Explicit Gmail cloud-copy import/update/delete and linked sending are covered by
the [completion slice](m1-gmail-completion.md). Automatic bidirectional cloud draft
synchronization, aliases, account identity reassignment, automatic content-matching
reconciliation, outbox pagination and SMTP remain separate tasks. The [conversation slice](m1-threads.md) now adds account-validated reply
parents, References chains and native Gmail thread IDs to outgoing replies. Failed payloads
cannot be edited in-place; cancel then compose a corrected message. An unknown
send must be checked before creating any replacement. Real Google authorization
and delivery have not been exercised in this slice; all automated sends use local
fixtures. See [validation](m1-gmail-outbox-validation.md).
