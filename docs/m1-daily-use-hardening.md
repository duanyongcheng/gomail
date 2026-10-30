# M1 Gmail daily-use hardening

This slice closes integration and reliability gaps in the existing Gmail app.
It does not implement the complete cloud-draft lifecycle, production IMAP/SMTP,
or the remaining upstream feature set.

## External-write durability

The profile database retains one writer and four read-only connections. Every
writer connection is configured for WAL and `synchronous=FULL`, including pool
replacement and database reopen. Reader connections remain read-only. This is a
connection policy, not a new schema migration.

Outbox and cloud copies still persist intent **before** any non-idempotent HTTP
write. A failed queue/intent statement or commit prevents submission. Interrupted
outbox sends remain unknown; unconfirmed cloud-copy writes block another upload.
No new automatic network retry or exactly-once claim is introduced. FULL requests
SQLite's commit durability; tests cannot prove physical disks/OS honor every
flush under arbitrary power loss.

## Desktop lifecycle

The Go desktop controller owns close behavior. The frontend sends the
profile-scoped close preference through a generated typed binding; it no longer
relies on a fake Tauri close-event subscription. Until that preference is known,
closing hides the window rather than destroying editing state.

- Background enabled: close hides the existing window and workers keep running.
- Quit-on-close enabled: close requests normal application shutdown.
- Application activation and a second instance restore/focus the same window.
  They do not open another profile, create another window or duplicate workers.
- Explicit Quit works independently of the close preference. Shutdown cancels
  application work; workers are joined after MyGO's event-loop cleanup has drained
  pending UI calls, before storage is closed. Joining UI-dependent work inside
  OnQuit would block that drain.
- Go owns initial visibility. Completing delayed frontend bootstrap does not
  reopen a window the user already closed.

Native lifecycle state/actions are serialized on MyGO's main thread. These basic
window behaviors do not claim a tray, notification or autostart implementation.

## Safe user-clicked web links

Message-body and About web links use one explicit command adapter and generated
`AppService` method. The Go boundary accepts valid HTTP(S) links only, rejects
credentials, malformed hosts/ports, invalid escaping, control characters and
backslashes, and calls the OS opener once. Invalid inputs never reach that opener.

Opening failure is visible to the user. Neither full URLs nor raw OS error text
are logged or reflected in the error toast. Opening a link is not a reputation
check or a promise the destination is safe. Existing in-app mailto composition is
separate from HTTP(S) opening.

## Honest feature availability

`src/lib/availability.ts` centralizes product support. A startup-stub is not an
implemented capability. Generated Gmail services are explicit exceptions to the
legacy invoke inventory; Gmail permanent deletion remains unavailable even
though the compatibility command exists to reject it.

Unsupported settings, navigation, advanced filters, provider setup and message/
compose actions are hidden or disabled with an explanation. Restored navigation,
command-palette execution and keyboard paths must obey the same policy. Working
Gmail account editing/reconnect/labels, sender trust, basic search, encrypted local
compose, cloud copies, outbox and local appearance/shortcuts remain available.

Message actions use account-scoped cached trash membership even when opened via
Search or Starred, not only the originating folder. Already-trashed messages
cannot report a repeated move-to-trash as successful deletion; Restore remains
available. Batch actions recheck membership after confirmation.

A pristine automatically inserted signature no longer blocks opening local drafts.
Pristine status uses the exact initialized editor content, not a signature marker
alone; actual edits and pending recipients/attachments remain protected. Draft
loading rechecks that protection before applying an asynchronous result.

Account/provider authorization still applies separately: product availability
never grants a disconnected account permission to perform a remote operation.
No editable IMAP/POP3/Outlook or proxy setup form should imply those transports
are connected to the production app.

## Verification scope

Regression coverage must use actual command/window preference adapters, not
replace a pending command or no-op event subscription with a working mock.
Storage tests inspect real connection pragmas and exercise faulted intent writes
against the local Gmail fixture, then reopen the database and check recovery.

The isolated packaged selftest substitutes only the OS web opener, never opening
a real URL. It clicks real message links, checks visible errors, closes/restores a
window with an edited draft, and launches a second instance to exercise the
single-instance callback. Quit-on-close is tested last through native shutdown.
Selftest uses a distinct application name/lock and disposable profile.

Final gate results and platform limitations are recorded in
[hardening validation](m1-daily-use-hardening-validation.md); this behavior contract
alone is not evidence that a later worktree has passed validation. Live Gmail, real keyring and
Windows/Linux runtime checks remain separate acceptance work.
