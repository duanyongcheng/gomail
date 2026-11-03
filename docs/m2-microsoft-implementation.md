# M2 Microsoft implementation and synthetic verification

Date: 2026-11-02. This records implementation and synthetic evidence only. It does not record live Microsoft consent, tenant policy, real delivery, system keyring, or personal/organizational acceptance.

## Implemented production paths

- `internal/oauth/microsoft.go` implements validated public-cloud tenant/audience/client configuration, native public-client S256 PKCE, bounded state/loopback callback/cancellation, explicit resource-specific token requests, independent Graph/Exchange grants, serialized refresh, rotated refresh families and persist-before-expose callbacks. Microsoft authorization does not inherit Google's authorization parameters. Exact XOAUTH2 framing has bounded challenge handling.
- `internal/app/microsoft_accounts.go` supplies typed Authorize/Status/Cancel methods. Account creation and reconnect verify the stable Graph mailbox identity, including the separately authorized Exchange identity, then write encrypted account configuration through the production account/CAS path. Failed, canceled, conflicting and wrong-mailbox authorization retains existing data.
- `internal/app/microsoft_providers.go` registers lazy production Graph and IMAP/SMTP factories. Restore constructs token sources without requesting tokens or opening connections. Token refresh merges changed resource grants through the account's serialized encrypted `UpdateConfig` transaction. Account generation checks fence network activity. IMAP and SMTP each request their own Exchange permission; Graph reading and sending use separate required scopes. Selected-folder edits use the same durable configuration callback as standard IMAP accounts.
- `internal/provider/graph/api.go` supplies verified HTTP profile/folder/delta/raw MIME/flags/move/folder CRUD/send/Sent-check methods. It uses the resolved mailnet transport and refuses redirects, unsafe continuation origins/routes, malformed IDs/receipts, oversized data and loops. Immutable IDs are requested consistently. Pinned sessions use the same access token for sender verification and submission.
- `internal/provider/graph/provider.go` walks bounded nested folders and resolves well-known roles by authoritative remote IDs. Per-folder cursors, continuation hashes, cache data, identity mappings, intent overlays and membership tombstones commit transactionally. Partial metadata preserves known flag state; raw MIME is hydrated through the shared parser and attachment/thread/privacy pipeline. Expired delta cursors retain old readable cache while a replacement traversal records a durable seen set, followed by bounded database anti-join reconciliation.
- `internal/store/graph_reconciliation.go` implements bounded missing-membership pages; generic checkpoint/mapping/clear-scope transactions are owned by the account implementation.
- `internal/provider/graph/sending.go` verifies the configured mailbox and stable subject with one pinned session, validates MIME From, and requires the durable pre-send callback. MIME retains Bcc for Graph recipient submission. Exact HTTP 202 is accepted without a RemoteID and is not delivery evidence. Lost/ambiguous receipts remain unknown. Explicit Sent checks require a unique Sent-folder Message-ID candidate plus matching parsed MIME sender and identity; they never resubmit.
- Remote message changes use the production pending-op claim barrier. Folder changes use the integration service's journal and `ChangeFolder` barrier. System folder protection uses authoritative well-known IDs rather than display-name guesses.

## Bounds and recovery

Metadata responses: 4 MiB. Raw MIME: 50 MiB plus shared decoded/part limits. Hydrated retained batches: 8 MiB or one bounded larger message. Supported Graph MIME submission: 3 MiB raw / 4 MiB base64 wire; the composer may support larger mail for other providers, so oversized Graph mail is refused before the send barrier. Folder enumeration: 100 pages / 5,000 folders. Delta pass: ten pages/transactional reconciliation batches. Continuation hashes: at most 10,000 per traversal. Reset reconciliation: 100 membership rows per transaction.

These limits deliberately fail closed. A failed MIME hydration or local transaction does not advance its delta boundary. Folder tombstones remove membership rather than globally deleting the account's cached message. Unknown writes are quarantined by the shared app/store lifecycle; local settlement retry never invokes the remote call again.

## Files and tests

Microsoft-specific additions include the OAuth implementation/tests; typed Microsoft service/tests; lazy factory/tests; Graph API, provider, sending and tests; bounded store reconciliation helper; and this report. The narrower OAuth report is `docs/m2-microsoft-oauth-implementation.md`.

Executed on the combined working tree:

- `go test ./internal/app ./internal/oauth ./internal/provider/graph` passed.
- `go test -race ./internal/oauth ./internal/provider/graph ./internal/app` passed (OAuth 2.279s, Graph 4.635s, app 72.122s).
- `go vet ./internal/oauth ./internal/provider/graph ./internal/app` passed in that same completed command.
- A later Graph recovery-only race run passed (4.499s) after adding cursor expiry and system-folder/barrier fixtures. Final targeted factory checks are recorded by the integration session after this report's initial write.
- Final `go test -race ./internal/oauth ./internal/provider/graph ./internal/app -run "Microsoft|Graph" -count=1` passed (1.883s / 3.562s / 4.847s), followed by `go vet` for those packages. This includes the actual registered Graph/IMAP/SMTP factory lazy-construction and generation-fence test.
- `go test -race ./internal/provider/imap -run Microsoft -count=1` passed (1.925s). `internal/provider/imap/microsoft_auth_test.go` exercises exact XOAUTH2 authentication over an explicitly trusted disposable TLS listener and rejects a password/mechanism fallback.

Fixtures use local TLS servers or in-process HTTP transports and disposable databases. Coverage includes PKCE/cancel/state, scope provenance, token rotation/CAS loss, resource isolation, stable identity mismatch, actual typed account creation/restore, opaque Graph IDs, malformed/unsafe continuation links, pagination loops, partial metadata, membership removal, expired cursor reconciliation, metadata/raw wire bounds, write receipts, send 202/rejection/response loss, one-attempt submission, pinned tokens, Bcc MIME, sender matching and read-only Sent checking.

## Integration and manual boundaries

The main integration session owns generated bindings/adapters/UI availability, the full `bun run check`, signed local build, packaged disposable desktop selftest and final review. Those checks are not claimed here from focused Go tests. Both delegated Microsoft children completed before this coordinator report.

Outgoing terminal authorization errors carry a safe `SendResult.AccessState` to the integration layer. Its per-account outgoing-resource state pauses only sending without consuming the preparation attempt or pausing healthy reads. `MicrosoftStatus.sendState` surfaces this state. Token/Graph read and send scopes remain independent; Graph's send-only permission loss does not block its read token lease. Definitive HTTP 401/403 send rejection may carry resource access state; an ambiguous outcome never does.

Microsoft client registration and public-client redirect/audience configuration, real personal/organizational consent, tenant SMTP AUTH policy, actual resource expiry/revocation, user-controlled delivery/Bcc receipts and system-keyring acceptance remain manual test-account prerequisites. No normal application was launched, no user's mailbox/profile/keyring was accessed, no credentials were read from `.env` or `auth.json`, no dependency was changed, and no commit was made. The Rust reference and loopback PoC guards remain untouched.
