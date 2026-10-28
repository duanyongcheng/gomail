# M1 local encrypted drafts and production MIME

This slice prepares compose for sending; it does **not** enable Gmail sending or
cloud draft synchronization. The compose page explicitly says local-only, and
Gmail's Send button remains disabled by its capability flag.

## User flow

1. Compose a message. After three seconds without changes, it is saved locally.
   The status distinguishes saving, saved and failed; failure leaves the editor
   open. No new plaintext draft is written to WebView localStorage.
2. **Save and close** flushes the current snapshot, waits for persistence, releases
   temporary attachment references and closes only on success.
3. Open a blank compose and expand **Local drafts** to list the latest 100 drafts
   for the selected From account. Open one to restore its body, recipients and
   attachments; later saves reuse the same ID.
4. Delete a draft from this list with confirmation, or discard the active compose.
   A failed delete does not close the active editor or clean its staged attachments.

Drafts may contain incomplete recipient input. To/Cc/Bcc (including pending text),
subject, text/HTML, editor mode, raw source, reply ID and quoted reply HTML are
preserved. Quoted HTML is kept separately from editable text and sanitized when
rendered. Markdown/HTML drafts initialize the editor from their source so switching
back to rich mode does not start with an empty document.

## Storage and security

Schema v25 adds `compose_drafts`, `compose_attachments` and
`compose_draft_attachments`, separate from remote messages and FTS. Their content
uses the existing AES-256-GCM envelope and profile DEK:

- Draft purpose: `compose_drafts.data`, record: `accountID + NUL + draftID`.
- Attachment purpose: `compose_attachments.data`, record: attachment ID.
- Subjects, recipients, bodies, filenames and attachment bytes are encrypted.
  IDs, account routing, timestamps, revisions and sizes are metadata in plaintext.
- A missing/locked keyring does not create a replacement key if credentials,
  drafts or staged attachments already contain encrypted data. Wrong-key/tamper
  failures retain existing ciphertext instead of overwriting it.

The compatible string `attachmentPaths` now contains opaque
`compose-attachment:<random-id>` references, never arbitrary filesystem paths.
Cleanup marks referenced objects released; bytes are removed only after the final
draft reference is removed. Updating/deleting drafts and replacing references
are transactional. Account deletion cascades drafts and collects released objects.

Each compose/account gets a stable client key, hashed in storage. Create retries
resolve to the original draft ID. Store updates use revision CAS. Delete retains
a routing tombstone (without content) so late saves cannot resurrect it. The
frontend serializes saves per account and never carries an old account's draft ID
into a different From account. This does not implement multi-window conflict UI;
the current app has one compose editor.

## Limits

- One staged attachment: 10 MiB.
- One draft: up to 100 attachments and 25 MiB total attachment bytes.
- Draft body/source/quote fields combined: 2 MiB.
- Stored staging objects: 200 MiB decoded bytes in total (ciphertext/base64 adds
  overhead). Explicit cleanup deletes unreferenced objects; hard-crash orphans
  are not yet reclaimed by a background TTL job.
- Local drafts list: latest 100; no pagination in this slice.

The backend remains the limit/security boundary. Frontend checks file sizes before
reading/serializing them. Legacy filesystem attachment paths are refused, so an
old localStorage draft with such attachments must reattach the files; its legacy
copy remains until a successful save.

## MIME foundation

`mailmime.BuildOutgoing(Outgoing, limit)` replaces test assumptions with explicit
From, Date, Message-ID, To/Cc/Bcc, reply/reference headers and deterministic
text/plain → text/html alternatives inside multipart/mixed. Attachments use the
existing MIME library. Header injection and invalid UTF-8 are rejected; encoded
output is bounded during writes (maximum 35 MiB).

`IncludeBcc` is explicit: Gmail raw submissions need routing Bcc headers while
SMTP must use envelope recipients and omit that header. The future provider must
choose correctly. The caller supplies a stable Message-ID across retries. The old
`Build` remains only a deterministic fixture/protocol-prototype helper.

No outgoing MIME is submitted to a server in this slice. Gmail draft APIs, sender
identity verification, durable outbox/reconciliation and send errors remain next.

## Tests

See [validation](m1-compose-drafts-validation.md). Typed bindings implement four
existing commands: save/delete draft and stage/cleanup compose attachment. The
invoke inventory stays 145, now with 42 implemented commands. Local list/get use
direct generated services rather than adding a generic dispatcher.
