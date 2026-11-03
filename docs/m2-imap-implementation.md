# M2 production IMAP implementation evidence

Implemented 2026-11-02 in `internal/provider/imap`. No live mailbox, normal user
profile, keyring, external network, credentials file, dependency upgrade or commit
was used by this provider worker. Existing prototype and Rust reference untouched.

## Files and production API

- `provider.go`: validated configuration, shared mailnet verified TLS/STARTTLS and
  proxy routing, password/XOAUTH2 authentication, folder discovery/reconciliation,
  local folder reads, explicit connection test, cancellable IDLE watcher.
- `sync.go`: bounded selected-folder synchronization, incremental and refresh
  cursors, atomic cache/checkpoint commits, MIME parsing/recovery, notifications.
- `writes.go`: durable local flag entry, pending operation replay, identity checks,
  native MOVE/safe UIDPLUS fallback, explicit folder CRUD barrier.
- `provider_test.go`: isolated TLS/STARTTLS servers and real SQLite fixtures.
- `microsoft_auth_test.go`: Microsoft worker-owned TLS XOAUTH2 wire fixture.

Factory: `New(db, account, domain.ProtocolConfig, globalProxy) (*Provider,error)`.
Hooks: `Token(context.Context)(string,error)`, `BeforeRequest(context.Context)error`,
`UpdateSelectedFolders(context.Context,oldRemote,newRemote string)error`.
The last callback persists encrypted selection changes before rename/delete
reconciliation clears its durable folder journal. Empty new remote means delete;
if removing the final selected folder, configuration must select INBOX.

Interfaces: domain.Transport, domain.ProgressTransport, cached `Folders(ctx)`,
explicit `RefreshFolders(ctx)`, `Test(ctx)`, `Replay(ctx,opType,payload)(unknown,error)`,
`ChangeFolder(ctx,action,source,name,beforeWrite)(unknown,error)`, and
`Watch(ctx,notify)error`. Root integrates typed account/folder services, SMTP send
composition, pending worker recovery and scheduler cancellation/backoff. No
provider goroutine starts during construction or restore.

## Implemented guarantees

- Remote IDs encode exact mailbox name plus UIDVALIDITY and UID; local IDs add
  account scope. Replay rereads the cached scoped identity, selects the mailbox,
  compares UIDVALIDITY and confirms UID existence before mutation.
- Eight numeric UIDs per page, ten pages per pass. Each pass is bounded to one
  minute; continuation has a transactional traversal cursor across selected
  folders and process reopen. Incremental fetching catches new UIDs; bounded
  refresh sweeps reconcile external flags and successful UID-FETCH absence.
- Complete bounded LIST allows at most 256 mailboxes, and absent folders only
  remove folder memberships through the shared repository. Cached message rows
  are not account-globally erased merely because LIST omitted a mailbox.
- UID epoch retirement reads at most 64 cached remote identities per page. Epoch
  changes during cleanup restart bounded reconciliation rather than retargeting
  old intents. Known app-driven mailbox rename preserves the stable local folder
  ID, persists selection before journal retirement, and retires old UID identities.
- Cache, attachments, FTS, overlay effects, recovered/deferred content and provider
  checkpoints share CommitSnapshot. The SQLite checkpoint fault test proves no
  cached message escapes a rolled-back page. Failed MIME preserves prior readable
  content and records a deferred item; later bounded refresh retries it.
- Metadata is streamed before body fetches. Oversized advertised messages are
  deferred without downloading their body. Individual body literals are limited
  before allocation even if a peer ignores the requested partial size. Shared MIME
  limits bound decoded data, parts and attachments. Maximum body is 25 MiB.
- Read/star uses the application's atomic local mutation queue; replay is
  idempotent. Move/trash/archive use a persisted exact target mailbox and the
  application's durable claim/barrier. Native UID MOVE is preferred. Servers with
  UIDPLUS use sequential UID COPY acceptance, UID STORE Deleted and UID EXPUNGE
  for that exact UID. No global EXPUNGE and no pipelined early source deletion.
  After accepted COPY, every later failure is unknown. Interrupted non-idempotent
  operations require the root worker's unknown quarantine, never automatic replay.
- Folder CREATE/RENAME/DELETE requires an account-owned non-system source and a
  successful caller-supplied durable barrier before the command. Unknown results
  remain for read-only folder reconciliation; this provider does not replay them.
- IDLE has one connection for the first selected folder, a buffered/coalesced
  notification, disconnect reporting and joined cancellation. Other selected
  folders remain covered by scheduled bounded sync. Scheduler owns manual mode,
  one-worker policy, reconnect backoff and stop/delete/shutdown lifecycle.
- Inbox announcements occur only for newly cached unread incremental messages,
  after page commit. Backfill/refresh do not announce historical mail or replay.

## Synthetic checks actually run

Final command passed:

```sh
go test -race ./internal/provider/imap -count=1 -timeout=90s
go vet ./internal/provider/imap
```

The final race suite completed in 11.671 seconds. `git diff --check --
internal/provider/imap` passed on the preceding provider revision; root performs
the combined repository gate after integration.

Fixtures exercise:

1. 106 messages across selected folders, unselected mail excluded, ten-page
   bound, reopen continuation, incremental arrival, MIME address names and expunge.
2. Fetch failure leaves incoming checkpoint unchanged; UIDVALIDITY replacement
   rejects stale writes and retires old cached identities.
3. Immediate local read/star, actual queued replay, authoritative refresh, MOVE
   disconnect returns unknown with one remote attempt.
4. Actual verified STARTTLS login, folder create/rename/delete, barrier failure
   makes zero writes, protected INBOX and context cancellation.
5. Actual IDLE notification and cancellation/join with a synthetic server.
6. Invalid identities, bad credentials and untrusted CA rejection.
7. Injected SQLite checkpoint trigger failure rolls cache back; reopen/retry works.
8. Oversized metadata causes zero body requests; later content recovery works.
9. Selected-folder rename config persistence failure retains journal; retry keeps
   local folder ID, updates selection and synchronizes without duplicated mail.
10. Native MOVE and UIDPLUS fallback succeed; server lacking both safely refuses.
11. Initial backfill suppresses announcements; incremental commit announces once.
12. Microsoft worker's TLS XOAUTH2 wire fixture verifies exact user/control frame,
    mechanism and independent IMAP token, with no password fallback (race pass).

## Explicit limits and remaining acceptance

- This worker did not run full `bun run check`, the signed packaged desktop build,
  or typed UI smoke; those are root integration responsibilities.
- Microsoft resource grant/refresh belongs to the Microsoft/integration slice.
  Its worker also added and race-verified this package's independent IMAP XOAUTH2
  wire fixture. The main sync fixtures authenticate with synthetic passwords.
- Server compatibility needs dedicated-account manual acceptance, including
  trust/proxy configuration, real IDLE reconnect behavior and folder special-use
  roles. No live consent, keyring or mailbox acceptance is claimed.
- Without MOVE or UIDPLUS, safe remote moves are unavailable. Ordinary flags and
  reads still work. Missing archive/trash role configuration must remain explicit
  in account/action availability instead of inventing a destination mailbox.
- IDLE watches the first selected folder; periodic sync covers additional folders.
  Numeric UID windows bound memory but sparse very large UID spaces can require
  many continuation passes. No CONDSTORE/QRESYNC optimization is claimed.
- Unknown moves/CRUD deliberately require reconciliation/user action. Exactly-once
  delivery or arbitrary server-side rename identity continuity is not claimed.
