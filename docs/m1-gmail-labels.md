# M1 Gmail server label management

Settings → Accounts → **Gmail labels** now offers cached label listing, explicit
server refresh, creation, rename and deletion for the selected Gmail account.
These are Gmail server labels represented by the existing `folders` cache, **not**
the separate M3 local tags (`list_labels`, `add_message_label`, etc.). The legacy
145-command inventory stays unchanged; new calls use generated `GmailLabelService`
bindings directly.

## User-visible behavior

- Opening settings reads only the local cache. **Refresh from Gmail** is an
  explicit network action; normal mailbox sync also refreshes label metadata.
- Create/rename/delete are online operations. The cache changes after remote
  confirmation rather than displaying an unconfirmed optimistic label.
- Names are trimmed, require 1–225 UTF-8 bytes, reject control characters and
  reserved system names, and cannot duplicate another label name ignoring case.
- Only custom labels can be renamed or deleted. System labels such as Inbox,
  Sent, Trash, Spam, Unread and Starred are protected again in the backend.
- Delete requires confirmation and **never deletes mail**. It removes the label
  and cached memberships, preserving messages, search content and attachments.
- A label referenced by an unfinished mail operation cannot be deleted here;
  first let that operation finish or dismiss its failure in Remote Writes.
- If a selected folder disappears, the sidebar returns to an available Inbox
  after a successful folder read instead of staying on a dead selection.

Custom labels appear before system labels in a scrollable settings list. The
manager exposes loading, read/write errors, busy state and account selection;
late results from another account cannot replace the current manager.

## Uncertain outcomes and recovery

V28 adds `gmail_label_changes`: one unresolved management intent per account,
with an opaque operation ID, action, target remote ID/name and creation time.
The intent is committed **before** issuing the write.

A management request does not automatically retry or follow redirects. A definite
4xx rejection (except 408) clears its intent; deletion's 404 is already absent.
Network/read errors, cancellation, malformed success, 408/5xx or redirect leave
the intent visible for reconciliation. If Gmail succeeds but the local commit
fails, the same intent remains rather than replaying the write.

Refresh is read-only and reconciles by observing the complete remote label list:

- create: exactly one custom label with the requested name;
- rename: the same remote ID now has the requested name;
- delete: the target remote ID is absent.

Unmatched outcomes remain blocked. A user can explicitly confirm they checked
Gmail and clear the record; this does not undo a remote operation and does not
itself retry it. A later manual retry can create a duplicate if the user clears
an unresolved create incorrectly. Name-based create reconciliation establishes
that the desired label exists, not which client created it.

Management/dismiss requests serialize in the application; the existing provider
gate serializes label reads/writes with mailbox sync, sending and queued mail
replay. Preflight checks the account's primary email and pins the same access
token for label listing and mutation. No new scope beyond `gmail.modify` is used.

## Folder and queue reconciliation

Only validated unpaginated lists (unique IDs, valid types and system Inbox present)
are used as deletion snapshots. Cache reconciliation is account-scoped and
transactional. Missing custom labels are removed; absent system fallback folders
are preserved. Rename retains stable local/remote IDs and memberships.

External deletion can invalidate previously queued moves. Reconciliation retains
those operations as visible failed rows at their retry budget, rather than
submitting invalid target labels forever. Replay rechecks target existence after
acquiring the provider gate, including rows claimed before a label refresh.
Deletion intents also block new moves to their target in the queue transaction.

Label metadata is reconciled before message pages; a later message download error
does not undo a successful label refresh. The app emits `mail:folders-changed`
after label management and sync attempts so sidebar, message/thread lists,
unread counts and pending-operation views refresh even in that partial-success
case. These are invalidation events, not a claim that every sync step succeeded.

## Boundaries

The new table contains ordinary label names/IDs as plaintext profile metadata,
like `folders`; it contains no OAuth tokens or message content. Legacy v20 tables
and schema oracle remain unchanged. There is no offline label worker, nested-label
recursive rename/delete, color/visibility editing, batch labels CRUD or service
filter management. Real Gmail consent/label semantics remain manual verification;
automated validation only uses local fixtures.

See [validation](m1-gmail-labels-validation.md).
