# M1 conversations and reply threading

The existing Conversation view now reads real local data through typed
`ThreadService.list/messages` bindings. `list_threads` and `list_thread_messages`
are implemented; the invoke inventory remains 145 (45 implemented).

## Grouping and account boundaries

Gmail's native `threadId` remains authoritative **within its account**. A native
thread in mailbox A can never merge with the same remote ID in mailbox B. Public
thread handles encode an account/kind/key tuple; bare remote IDs are rejected.
The handle is an opaque identifier, not a cryptographic authorization token.

V27 adds `message_threads` with a message FK, account FK, grouping kind and key.
The original `messages.thread_id` retains the native provider value for sending.
Migration indexes existing cached native threads immediately. No legacy v20 table
changes are needed; the independent schema oracle remains intact.

Mail without a native thread uses normalized Message-ID/In-Reply-To/References
links. Missing ancestors have placeholder graph nodes, so receiving replies before
parents still groups them. Cycles terminate; duplicate IDs/links deduplicate. No
subject-only heuristic is used. Different native threads never merge just because
their headers reference each other. The fallback graph currently covers only mail
without native IDs, not bridges between native and header-only messages.

Header-only grouping is rebuilt per affected account when fallback metadata changes,
using only header columns, never message bodies. Native updates are incremental.
Bridge arrivals can change a fallback handle; refresh/reselect that conversation.
Soft-deleted messages retain link metadata but never appear or contribute to counts.
Physical deletion/cascade removes the index; a later fallback update can rebuild
components. Very large non-native mailboxes may need incremental graph maintenance
before IMAP integration.

## List and reading behavior

- Summary counts, unread/star/attachment indicators, participants and latest
  subject/snippet are scoped to the selected folder(s). A message in two selected
  folders counts once. Equal dates use a stable message-ID tie break.
- Selecting a conversation displays all its live cached messages in that account,
  including those in other folders (e.g. Inbox + Sent), in date/ID order.
- Lists page in groups of 50 with a Load more button; overlap is deduplicated if
  arrivals shift offset pages. Keyboard next/previous uses active paginated cache.
- Detail returns metadata without body strings. Expanding a bubble fetches backend
  privacy-rendered HTML, supports retry, ignores stale responses and marks only
  opened unread messages through the durable mutation queue.
- Reply/Reply all/Forward fetch the full original message for quotes. Attachments
  use the existing safe attachment UI. Reply uses the original message's account
  by default, including from the combined inbox.
- A detail with more than 500 cached messages is rejected rather than silently
  truncated. Switch to message view; per-thread incremental pagination is future
  work. Summaries can therefore differ from detail counts by folder scope.

## Reply context and sending

MIME parsing and snapshot persistence now retain Cc/Bcc, In-Reply-To and References.
Optional malformed thread headers are ignored without rejecting otherwise readable
mail. Header lists are bounded to 16 KiB and 100 IDs, retaining root/recent refs.
Already cached bodies did not preserve these headers in older builds; re-fetching
a message fills them in. Migration cannot reconstruct headers that were not stored.

Compose/draft/send requests may carry a local `replyMessageId`. Backend resolves
that message in the sending account, rejects cross-account parents, derives the
parent Message-ID and appends the References chain (root + closest ancestors under
100 entries). The client cannot submit an arbitrary provider thread ID.

When the reply subject matches after removing common reply/forward prefixes, the
outbox saves the native Gmail thread ID alongside immutable MIME and the send JSON
includes `threadId`. Changed subjects omit it. Gmail remains the final authority
on its own grouping. Older queued outbox payloads lacking thread context still
send as before; unknown-outcome and single-POST protections remain unchanged.

Local drafts preserve the parent message ID. If that parent is deleted before
queueing a reply, the send is rejected rather than guessed. Raw legacy inReplyTo
requests still send headers without a provider thread context.

See [validation](m1-threads-validation.md). Real Gmail grouping, cloud drafts,
label management, and IMAP integration remain separate verification/work.
