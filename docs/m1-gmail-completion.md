# Gmail daily-use completion

Implementation and integration work dated 2026-10-29. This contract does not
replace test evidence: see the completion validation report when available.

## Opening mail and read state

Opening a message reads local metadata separately from privacy-rendered HTML.
The read mutation no longer waits for the body renderer. Expanded conversation
messages fetch fresh local metadata on each expansion; an old cached read flag
cannot consume the opening before that read completes. They use the same
flag-mutation/cache path as standalone details, keyboard and command-palette actions.

The local SQLite change and durable remote-write intent remain one transaction.
The UI updates the selected message in ordinary lists, starred results, detail
and conversation-message caches, rolls back only affected fields on failure,
and refreshes unread counts after the local acknowledgement. Sync/queue events
defer message-cache refetches until pending flag mutations settle, so they cannot
replace provisional flags with stale results. Gmail replay is a
background operation with immediate wake-on-enqueue, not a prerequisite for
displaying the locally persisted read state. Metadata and flag IPC continue even
when the WebView reports offline; Gmail availability affects remote replay, not
local SQLite acknowledgement.

Opening is a one-time action for the current selection/expansion. A later manual
mark-unread must not be immediately undone by a refetch while the message remains
open. Navigating away and opening again may mark the message read again.

This is not a promise that the Gmail website changes instantaneously: offline
operation, authorization errors and queued remote work remain visible in Remote
Writes. Reducing polling intervals is not the fix for local acknowledgement.

## Explicit Gmail draft access

The Gmail draft browser lists remote drafts in bounded pages. Import reads the
selected draft and saves a separate encrypted local compose snapshot, including
Bcc, attachments and reply context. Browse/import do not modify Gmail. Local
autosave and opening Local drafts remain offline.

Remote draft IDs are stable resource identities, not cached DRAFT message IDs.
The app never guesses a draft ID from a message. Import records both the draft
ID and the replaceable message ID, plus an encrypted semantic MIME digest.
Missing Date/Message-ID headers in unfinished drafts are handled by a dedicated
draft parser rather than weakening received-message validation.

Imported attachments retain MIME type, Content-ID and inline disposition in
encrypted staging. Their bytes, local draft, references and cloud association
commit atomically and follow the existing size/quota limits. Primary-mailbox
sender validation remains required; importing an unsupported alias does not
silently reassign its sender.

## Editing and conflict recovery

Upload/Update publishes only a saved local snapshot. Markdown saves preserve the
source and its corresponding rendered HTML; stale or legacy source without a
matching rendered snapshot is not silently uploaded as formatted mail.

Mode conversion is checked before changing the editor or save snapshot. If rich
text or Markdown cannot preserve imported HTML, such as CID images, tables or
unsupported formatting, the mode stays unchanged and a localized warning is
shown. Continue editing/saving/sending in HTML mode; there is no silent lossy
conversion. Refusal does not change the source, quote, selection or undo history.

Before updating or deleting an associated cloud draft, the app reads and compares
the known remote message ID and semantic content. A changed or missing draft is
a conflict, not permission to recreate, overwrite or delete it.

Explicit recovery offers:

- Keep local version: retain local content and detach a known conflicted cloud
  association without changing Gmail. A later explicit upload creates a separate
  Gmail draft.
- Keep both versions: preserve the old local draft and import the current Gmail
  version into a separate local draft, transferring the cloud association in one
  transaction.

Neither action discards an unresolved write/send journal. Unknown outcomes must
be checked rather than transformed into a blind retry.

Gmail's documented draft API does not provide an atomic expected-revision
condition for these writes. Preflight detects changes already observed, but a
web edit can still race between the read and mutation. Do not edit the same
draft simultaneously in Gmail and Pebble. This is not an atomic multi-client
synchronization engine.

## Remote deletion

Delete Gmail copy is a separate, explicitly confirmed permanent deletion, not a
move to Trash. The local draft remains. A durable deletion intent precedes the
single request; an unknown result blocks repeating the deletion automatically.
Read-only Check can settle a confirmed remote absence after response loss or
restart. Deleting only the local draft still does not implicitly delete Gmail
content.

## Linked draft sending

The UI supplies the saved local draft identity when queuing a send. A cloud-bound
send freezes the expected remote identity alongside the complete encrypted MIME,
and reserves the association with the outbox insert. The latest encrypted local
draft and attachment references remain protected through queued, failed and
unknown outcomes. Post-queue local cleanup is deliberately a no-op for a reserved
draft; it cannot be restored for editing, saved over or queued again.

Cancelling a queued/failed linked send releases that same local snapshot for
editing, without modifying Gmail. An already-open Local drafts list refreshes on
the native outbox event, with stale/account-switched reads ignored. After a
preflight conflict, cancel first, then
use Check Gmail copy and the normal conflict recovery actions. Confirmed Gmail
acceptance or Check Sent atomically records success and removes the protected
local draft content/references; the sent association and request-key tombstones
remain to prevent accidental reuse.

Older builds may already have deleted the local snapshot on queue. Cancellation
of those linked records now fails closed, retaining their last outbox MIME and
showing a specific explanation. It does not reconstruct lost editor source/quotes
from MIME or claim that cancellation succeeded; recovery of such legacy records
is a separate operation.

After verified-mailbox and remote-version checks, the worker persists its
pre-send state and submits the immutable MIME with Gmail drafts.send. Gmail
consumes the associated draft as part of accepting the send. Local-only mail and
legacy unbound outbox records continue to use messages.send.

There is no follow-up automatic remote DELETE and no fallback from a missing or
conflicted draft to messages.send. A network error, lost receipt or crash after
submission remains unknown and is not automatically resent. Check Sent retains
the stable Message-ID reconciliation path. Acceptance by Gmail is not a claim of
recipient delivery.

## Storage and compatibility

Schema v31 extends the Go-only cloud-copy journal with delete/send lifecycle
states and an outbox association. The Rust-compatible v20 schema is unchanged.
Import content, attachment metadata, source-render provenance and frozen send
identity remain inside the existing encrypted payload boundaries.

The independent Pebble-Go profile/keyring contract remains in force. No migration
from an old live Pebble profile is implied by this work.

## Acceptance boundaries

Automated tests use synthetic loopback Gmail, disposable profiles and in-memory
keyrings. The packaged desktop selftest exercises actual UI actions and native
bindings, including read acknowledgement while the synthetic server is stalled.

Live Google consent, real mailbox behavior, recipient delivery and system-keyring
behavior require separate acceptance. Cross-compilation alone is not Windows or
Linux runtime validation. Production IMAP/SMTP remains the subsequent M2 work.
