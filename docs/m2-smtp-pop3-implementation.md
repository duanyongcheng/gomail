# M2 SMTP and POP3 implementation evidence

This report records local synthetic implementation evidence on 2026-11-02.
It does not establish real mailbox acceptance, consent, system-keyring behavior,
recipient delivery or full application packaging. Those remain separate gates.

## Files and ownership

- `internal/provider/smtp/smtp.go`: production, verified TLS/STARTTLS SMTP client.
- `internal/provider/pop3/pop3.go`: production, verified TLS/STLS POP3 retrieval.
- `internal/provider/smtp/smtp_test.go` and `internal/provider/pop3/pop3_test.go`:
  disposable local protocol servers and temporary SQLite databases, implemented
  by the explicitly delegated fixture worker and reviewed through tests.
- `internal/domain/sending.go`: explicit `SendResult.Accepted` and frozen
  `OutgoingMail.EnvelopeRecipients`.
- `internal/app/outbox.go`: encrypted immutable envelope, provider-specific Bcc
  MIME policy, acceptance without RemoteID, settlement-only persistence retries.
- `internal/app/outbox_provider_test.go`: actual app queue/worker/SQLite tests.

Account configuration encryption, migrations, provider factory registration and
UI integration belong to the accounts/integration owners. These packages consume
their shared `mailnet` and atomic provider-state APIs; they do not use PoC tables.

## SMTP behavior

`smtp.New(Config)` is lazy. Config snapshots the endpoint and proxy. `Test`
authenticates without a send. Password PLAIN and explicit XOAUTH2 token sources
are separate; both require verified implicit TLS or mandatory STARTTLS. The
client repeats EHLO after STARTTLS, rejects missing AUTH mechanisms and never
falls back to another authentication mode. Tokens are resolved once per session.

Configured sender and MIME From must match the frozen account sender. All
To/Cc/Bcc envelope recipients are independently validated. SMTP MIME containing
Bcc or Resent-Bcc is rejected. International envelope addresses require the
server's SMTPUTF8 offer. Responses have line/count/aggregate limits; credential,
token and outgoing MIME sizes are bounded. Session deadlines remain bounded
after TLS upgrade and cancellation closes the underlying connection.

MAIL and every RCPT must succeed before the durable `beforeSend` barrier. A
rejected recipient closes the session without DATA, including when earlier
recipients were accepted. A failed generation check or durable barrier issues no
DATA. Once DATA is requested, connection loss is conservatively unknown. Lost
body/final response is unknown and never automatically retryable. Explicit
negative SMTP completion is proven nonacceptance; explicit 250 completion is
server acceptance, not recipient delivery, and returns `Accepted=true` without
inventing a RemoteID. No alternate protocol fallback exists.

SMTP alone cannot search Sent. `CheckSent` returns an inconclusive error and
never resends. An authoritative, unique Sent check would require the reading
provider's verified matching evidence; absence must not authorize a retry.

## POP3 behavior

`pop3.New(db, account, Config)` is lazy. Implicit TLS and STLS verify the configured
hostname and roots before USER/PASS. UIDL is validated before retrieving mail:
unique message numbers, unique printable 1-70 byte UIDLs, at most 100,000 entries
and 10 MiB of listing metadata. Protocol lines are bounded to 64 KiB. These are
explicit safety limits, so larger listings fail without destructive cleanup.

Messages are fetched one at a time with dot unstuffing and a wire/decoded MIME
limit capped at 50 MiB. A pass commits at most 100 new messages (configurable
downward). Stable identities derive from account plus UIDL, never the volatile
message number. Each message, attachment metadata, Inbox membership, UIDL mapping
and provider checkpoint commits atomically. A later failure preserves earlier
commits. Database reopen and remote UIDL reorder continue through the persistent
mapping. Generation checks run before connection and each snapshot commit.

No DELE command exists. Server disappearance never removes the local archive.
POP3 advertises read/HTML only and rejects remote flags. Sending is available
only with an explicitly supplied SMTP sender. Folder creation, moves, trash and
remote flags remain unsupported for POP3. Oversized/malformed messages abort the
current pass and preserve cache/checkpoints; correcting server content or limits
is required before that item can be imported. No automatic deletion or silent
data truncation is used to skip it.

## Outbox behavior

The existing encrypted payload now freezes all three recipient groups separately
from MIME. SMTP accounts build Bcc-free MIME; Gmail and Graph retain their raw
recipient policy. An old Gmail payload without the new field remains readable.
An old SMTP payload with no envelope cannot silently omit Bcc recipients.

`Accepted` permits SMTP/Graph acceptance without an ID while preserving Gmail's
existing nonempty-ID success. Unknown wins over contradictory acceptance, and
both sent and unknown force retryable false. A failed SQLite settlement retains
the result in memory and retries only SQLite; restart recovery still quarantines
an interrupted durable send. Existing cloud-draft paths are preserved.

## Executed verification

- `go test ./internal/provider/smtp ./internal/provider/pop3 ./internal/app` passed.
- `go test -race ./internal/provider/smtp ./internal/provider/pop3 ./internal/app -count=1`
  passed (SMTP 1.282s, POP3 4.530s, app 58.943s before the final generation-fence
  additions).
- `go vet ./internal/provider/smtp ./internal/provider/pop3 ./internal/app` passed.
- After generation-fence and post-TLS deadline changes, the fixture worker ran
  `go test -race ./internal/provider/smtp ./internal/provider/pop3 -count=1`:
  SMTP 1.494s, POP3 4.974s, both passed.
- Final verification after bounded-credential/SMTPUTF8 review:
  `go test -race ./internal/provider/smtp ./internal/provider/pop3 -count=1`
  passed (SMTP 1.611s, POP3 5.159s);
  `go test ./internal/app -count=1` passed (4.057s), targeted `go vet` and scoped
  `git diff --check` passed.

Fixtures cover SMTP TLS/STARTTLS acceptance with no ID, Bcc envelope privacy,
sender/Bcc rejection, partial RCPT rejection, failed durable barrier, lost final
reply, certificate rejection before AUTH, exact XOAUTH2 framing/token-source
separation and generation fencing before submission. POP3 fixtures cover
TLS/STLS, UIDL reorder/reopen, duplicate UIDL rejection, oversized mail preserving
old cache, cancellation of blocked UIDL, bounded continuation, certificate
rejection before USER/PASS, limited capabilities and generation fencing. A
temporary SQLite trigger demonstrates mail/mapping/checkpoint rollback together
and successful continuation after reopening and repairing the fault.

The app integration fixture verifies encrypted frozen Bcc recipients survive
queueing, raw SMTP MIME does not contain the Bcc address, and an accepted send
without RemoteID settles to sent after an injected SQLite failure with exactly
one transport invocation. Existing app Outbox tests also cover unknown restart,
unknown retry rejection, settlement failures, begin-send persistence faults,
queued restart and worker cancellation.

Follow-up resource authorization gate: SMTP preserves wrapped typed
`domain.AccessError` from the explicit token source and generation hook. Before
submission, terminal states return `SendResult.AccessState` with retryable false
and a static SMTP diagnostic. Generic network/password rejection does not invent
a reauthorization state. Four terminal-state fixtures verify no AUTH, DATA or
durable send barrier and no wrapped error-text disclosure. Final
`go test -race ./internal/provider/smtp -count=1` passed (1.527s) and
`go vet ./internal/provider/smtp` passed. Durable per-resource queue pausing is
owned by the integration coordinator.

## Remaining acceptance boundaries

No dependencies were upgraded. No commit, live account operation, real profile,
system keyring, live send or Rust-reference mutation occurred. Full `bun run
check`, signed packaging and packaged disposable-profile synthetic verification
are coordinator gates, not claimed by this worker. Dedicated server/client
configuration and real recipient/keyring acceptance remain manual. Generic SMTP
verifies a configured authenticated endpoint and configured sender; SMTP does
not provide a portable authoritative account-profile identity API like Gmail.
