# M1 Gmail cloud draft copies — first slice and completion

This document records the original upload/update/check slice. The completed
follow-up lifecycle is specified in [m1-gmail-completion.md](m1-gmail-completion.md)
and supersedes the historical incomplete-scope paragraph below.

## Available now

Compose → Save and close → open a blank compose → Local drafts. For a saved
Gmail draft, **Upload to Gmail** publishes its snapshot using `users.drafts.create`;
**Update Gmail copy** uses `users.drafts.update` with the saved stable draft ID,
not the replaceable Gmail message ID. **Check Gmail copy** performs reads only.
Opening the local list and local autosave remain network-free. There is no worker
that uploads drafts on startup or silently retries a cloud write.

Local status distinguishes local-only, uploaded snapshot, newer local edits,
unconfirmed upload, and changed/missing Gmail copy. An uploaded snapshot is not a
claim that the remote copy has remained unchanged since its last check. Local
content, source and attachment bytes retain the encrypted compose storage model.

The primary mailbox is verified with the same token pinned for the write. The
backend builds bounded MIME from the saved sender configuration, recipients,
body, Bcc, reply context and opaque staged attachments. Empty recipients are valid
in cloud drafts but invalid for Send. Incomplete address text remains valid
locally, and must be completed or removed before upload. Markdown drafts must be
switched to Rich Text and saved first; no lossy backend Markdown guess is made.

## Durable write boundary

Schema v30 adds `gmail_draft_copies`, account/local-draft scoped. It stores the
stable remote draft ID, replaceable message ID, local snapshot revision and state.
The per-write RFC Message-ID and semantic MIME digest are encrypted with AAD
`gmail_draft_copies.data` / `accountID NUL localDraftID`. Account deletion cascades
this local metadata; it does not delete any remote Gmail content. Legacy v20
schema remains unchanged.

1. Build the immutable snapshot under the local compose lock, then release it.
   Slow network I/O must not block local autosave.
2. For updates, fetch the remote copy and compare the known message ID and
   semantic digest. The digest includes decoded addresses, headers, bodies and
   explicit attachment hashes, ignoring Gmail-added transport headers.
3. Store a `pending` intent with revision CAS before the single POST/PUT. A local
   edit/deletion before intent creation prevents the upload. Edits after intent
   creation remain newer local edits, not falsely marked uploaded.
4. Disable redirects/replayable request bodies. No application write retry loop.
   Explicit rejection restores the previous copy state. Transport failures,
   malformed success, 408/5xx, redirects, crashes and failed receipt persistence
   leave a durable unconfirmed intent, blocking another upload and local deletion.
5. A read-only check fetches the known draft, or searches by the encrypted
   per-write Message-ID when create lost its receipt. Only one unpaginated match
   with verified matching content settles the intent. Missing, ambiguous,
   unparseable or modified content does not authorize a retry.

**Concurrency limit:** Gmail's draft update API does not expose a usable compare-
and-swap in this implementation. Preflight detects already-observed web edits,
but cannot prevent a web edit in the interval between GET and PUT. Do not edit the
same draft in Gmail and Pebble simultaneously. This is not multi-client conflict-
free synchronization. Unknown or conflicting copies have no force-overwrite UI.

## Historical first-slice boundary

At the time of the first slice, arbitrary draft import/browser, remote delete,
Markdown cloud rendering and cloud-copy cleanup were deliberately absent. Those
flows are now implemented by the completion contract. The product still does not
promise atomic bidirectional synchronization because Gmail exposes no documented
conditional revision/CAS operation; simultaneous web/Pebble edits remain a
documented conflict risk.

IMAP/SMTP remain the isolated protocol prototype. Their production integration
follows the completed cloud draft lifecycle; see
[gmail-daily-use-plan.md](gmail-daily-use-plan.md).

## Verification

Local fixtures cover create/update, Bcc and attachment preservation, empty
recipients, account/mailbox mismatch, invalid addresses, missing remote copies,
external edits, lost responses, durable restart guards, local receipt failures,
read-only reconciliation, encrypted identity/wrong keys and nonblocking autosave.
Store tests cover v29→v30/legacy oracle, scope, revision CAS, durable intent,
rejection rollback and cascade. HTTP tests cover no redirects, invalid IDs,
known rejections, malformed success and unknown outcomes for POST and PUT.
Frontend tests cover explicit upload, scoped requests, unknown-state gating,
read-only checks and visible failure. No real Google mailbox was accessed.

The first-slice `bun run check` gate passed (Go test/vet/race, MyGO vet, bindings,
TypeScript, Vitest, web build and five cross-compilation targets). Packaged macOS
arm64 build and strict codesign verification passed. Desktop selftest passed
26/26 including actual upload/check/update buttons; its screenshot was inspected.
A normal empty profile stayed alive and created schema v30 with zero accounts.
The completion validation also passes the expanded cloud lifecycle tests and
packaged synthetic UI selftest. Live Gmail, system keyring, Windows and Linux
runtime validation remain pending.
The existing Vite large-chunk advisory is unchanged. Final counts and evidence
are in [validation](m1-cloud-draft-copies-validation.md).
