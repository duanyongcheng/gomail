# M1 Gmail local account settings and signatures

Gmail account editing is local-only. Sender display name, account label and color
save together with an optional encrypted signature. The update is keyed by existing
account ID, confirms that the mailbox email is unchanged, and never rewrites auth
data, provider identity, order, sync checkpoints or queues. A stale form cannot
change or recreate a deleted account.

Signature ciphertext uses the profile DEK and AAD `email_signatures.data` plus
the account ID. The account-scoped v29 table cascades on removal and leaves the
Rust v20 schema oracle unchanged. Empty signatures are stored explicitly so a
later legacy import cannot restore a user-cleared value. The store returns the
winning value atomically when concurrent legacy migrations race with a save.
Before replacing a saved signature, the service authenticates its ciphertext;
a valid-length but wrong key, tamper or account mismatch cannot overwrite it or
partially change account metadata.

Settings reads signature content before enabling Save. If key access or decryption
fails, keep the dialog open, retain unsaved input, and retry the read; never submit
an absent fallback signature. A successful Gmail form save commits account fields
and signature in one transaction. Legacy browser data is removed only after this
write succeeds. Existing standalone signature commands remain available to the
compose UI and upstream compatibility layer.

Signature values are bounded to 64 KiB of UTF-8 text and reject NUL. Gmail proxy
settings are not wired and must not be saved as part of local profile edits. Mailbox
identity is read-only: authorization repair uses the existing same-mailbox Reconnect
flow. Aliases and mailbox reassignment are not supported.

New compose/reply editors fetch the current saved signature. Sending creates an
immutable encrypted outbox MIME snapshot using the current sender display name;
changing settings never alters queued or outcome-unknown messages.
Compose editing, autosave, Save and Send wait for signature loading and initial
content application. Read errors offer Retry without an empty fallback. Changing
From replaces only the marked automatic signature paragraph, retaining body text,
quotes and source-mode content. Rich/Markdown/HTML switching preserves its marker;
Markdown sends use rendered plain text rather than the embedded HTML marker.
Restored drafts preserve their saved signature until the account is changed.

## Validation

Backend tests cover transaction rollback, narrow column preservation, account
isolation/cascade, settings surviving database reopen, ciphertext tamper/AAD,
missing keys, explicit clears, legacy migration races, mailbox mismatch, and saved
name/signature in the outgoing MIME. Frontend tests cover no proxy dependency,
read/retry gating, duplicate-submit prevention, legacy preservation after failure,
and typed command parity. Real TipTap lifecycle tests additionally cover delayed
loading, failures/retry, account-switch races, empty signatures, source-mode
round-trips and restored drafts. The local desktop selftest exercises the account form
and encrypted signature without Google or the system keyring.

Real Gmail consent/delivery and other operating systems' runtime remain manual.
