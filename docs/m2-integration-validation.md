# M2 integrated implementation and synthetic validation

This report describes the November 2, 2026 production implementation. It does
not establish live mailbox, recipient delivery, Microsoft tenant consent or
system-keyring acceptance. Existing manual evidence remains unchanged in
`m1-live-acceptance.md`. No commit, dependency upgrade or live mailbox operation
was performed. The Rust reference and protocol PoC loopback guard remain intact.

## Implemented paths

Independent review subsequently corrected compatibility, refresh/test, Graph
intent/error handling, SMTP authentication and POP3 memory defects. See
`m2-review-validation.md` for the corrected files, current command inventory
and actual native WebView evidence. The implementation below and its historical
82-check package run are not a substitute for that review. The review's signed
arm64 run passed 89/89 checks, including seven separate real WebView M2 checks.

- Accounts: production IMAP/POP3 creation, explicit connection testing, safe
  settings projection, encrypted configuration replacement with observed-revision
  CAS, account generation fences and local-only lazy restore. Microsoft accounts
  enter through verified public-client authorization, not injected credentials.
  Vault resource updates merge under serialization and persist before use.
- Store: Go v32 provider/account/scope checkpoints and identity mappings, atomic
  message/checkpoint/membership commits, bounded mapping/reconciliation pages,
  rollback and reopen fixtures. The independent v1-v20 Rust schema oracle remains
  unchanged. Existing Gmail checkpoints, drafts and unknown sends are preserved.
- IMAP: selected-folder bounded backfill/incremental and flags/deletion sweeps,
  exact mailbox/UIDVALIDITY/UID identity, epoch retirement, bounded streaming
  literals and content deferral, durable queued flags/moves/trash, folder
  CREATE/RENAME/DELETE barriers and reconciliation. Rename retains local folder
  IDs and updates encrypted selected folders before clearing its journal.
  Scheduler-owned IDLE coalesces signals and cancels/joins on stop, preference
  change, deletion and shutdown. Manual mode never opens IDLE.
- SMTP: immutable encrypted Outbox MIME and To/Cc/Bcc envelope, Bcc-free SMTP
  MIME, configured authenticated sender checks, verified TLS/STARTTLS, separate
  password/XOAUTH2, all-RCPT acceptance before DATA, durable pre-send barrier and
  phase-aware failures. Lost submission receipts remain unknown with no retry.
  SQLite settlement faults retry SQLite only. Accepted submission can have no
  remote ID, while Gmail's existing nonempty-ID receipt stays compatible.
- POP3: verified TLS/STLS, bounded UIDL/RETR parsing, account/UIDL identity,
  UIDL reorder/reopen deduplication and atomic message/mapping/checkpoint writes.
  No DELE or remote flags/folder actions; optional configured SMTP enables send.
- Microsoft: validated public-cloud tenant/audience/client policy, PKCE/state,
  Graph/Exchange resource grants, scope-specific refresh and rotated refresh
  credentials persisted through encrypted CAS, independently scoped IMAP/SMTP
  XOAUTH2 and stable identity verification. Graph reads and sending request
  separate required scopes. Actual trusted-TLS fixtures verify protocol framing.
- Experimental Graph: bounded recursive folder enumeration, system roles,
  per-folder opaque delta continuations with strict canonical origin/operation
  validation, partial metadata hydration, raw MIME and membership tombstones.
  Expired cursors preserve readable memberships until completed bounded reset
  reconciliation. Desired flags and non-idempotent moves/folder changes use
  durable intent. sendMail exact 202 is accepted without an ID; unknowns remain
  quarantined. Explicit Sent checks require complete unique matching evidence.
- Network: verified TLS defaults, explicit endpoint CA trust, no plaintext or
  certificate-verification bypass, HTTP CONNECT/SOCKS5, inherit/direct/custom
  precedence and no proxy-failure fallback. Global, protocol/Microsoft account and
  Gmail account routes apply to application HTTP/token/profile/protocol traffic.
  The system browser follows OS routing. Network edits fence active authorization
  and rebuild lazy providers only after account work is quiescent.
- Resource permissions: terminal sending and queued-write authorization failures
  persist separate resource states, retain queue data without consuming attempts,
  and do not block healthy independent resources. Reauthorization clears those
  states without changing unknown sends. Local settlement failures cannot replay
  a remote operation. Safe Microsoft status displays both resource states.
- Frontend: actual typed add/edit/test, selected folders, CA trust, network,
  public-client authorization/cancel/status and guarded folder-management panels.
  POP3 mutation guards cover backend, lists, toolbar, batches and shared actions.
  Independent review bridges the remaining supported legacy M2 commands: the
  manifest now has 145 commands with 71 adapters; advanced search stays M3.

## Changed files

Detailed per-slice inventories and fixture results are in:

- `m2-accounts-implementation.md`
- `m2-imap-implementation.md`
- `m2-smtp-pop3-implementation.md`
- `m2-microsoft-implementation.md`
- `m2-microsoft-oauth-implementation.md`
- `m2-frontend-implementation.md`
- `m2-packaged-fixture-implementation.md`

Production additions are `internal/domain/protocol_accounts.go`,
`internal/app/{protocol_accounts,account_network,provider_factories,protocol_folders,microsoft_accounts,microsoft_providers}.go`,
`internal/store/{provider_state,provider_access,account_network,protocol_folders,graph_reconciliation}.go`,
`internal/store/migrations/032.sql`, `internal/oauth/microsoft.go`, and the
`internal/provider/{imap,smtp,pop3,graph}` packages. Existing app/store/domain
Outbox, pending queue, scheduling, restore and snapshot code integrates them.
New fault/reopen/resource/IDLE/protocol fixtures accompany those additions.

Desktop integration changes `main.go`, `selftest.go`, `selftest_hardening.go` and adds `selftest_m2.go`
with its headless test. Frontend changes the account/settings forms, shared
capability guards, message action controls, generated `src/mygo.ts`, explicit
command adapters/generator/manifest and parity. README and daily-use planning
now distinguish implementation, synthetic evidence and pending live acceptance.

## Verification evidence

The initial combined check found the real IMAP connection-test empty account-ID
bug through the new packaged scenario. It was fixed in the production service.
The next check found five outdated tests assuming the now-implemented proxy tab
was unavailable. Those assertions now verify enabled proxy navigation/restoration
while other unsupported features remain guarded.

A complete combined `bun run check` passed with artifacts at
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-SuFhtP`:
Go tests, vet, internal race tests, MyGO vet, bindings, TypeScript, 173 Vitest
files / 1221 tests, Vite and darwin/amd64, linux/amd64+arm64,
windows/amd64+arm64 cross-compilation. A final repeated combined gate after
Gmail account-route completion also passed at
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-24YmS7`:
all the same stages, with 174 Vitest files / 1224 tests. The only subsequent
source change updates the packaged hardening assertion to require the enabled
proxy tab while still checking unsupported tabs; its final signed rebuild
regenerated bindings, typechecked, built web assets and compiled Go successfully.

Targeted added tests verify resource permission isolation and unchanged attempt
budgets, durable folder barriers and reconciliation, protocol role destinations,
unknown interrupted moves, POP3 refusal and IDLE lifecycle/coalescing. Each
provider report records additional local TLS/HTTP fixtures and race results.

The local arm64 app was built and ad-hoc signed at
`build/darwin-arm64/Pebble Go.app`; build script strict deep signature verification
passed. It was launched only with `PEBBLE_SELFTEST=1`, the isolated selftest
application name, disposable profile and in-memory keyring. Final execution
exited zero and `/private/tmp/pebble-m2-packaged-final/results/runtime.json`
contains **82/82 true assertions**, including all seven actual M2 provider checks.
The first packaged run was 81/82 because its old unsupported-settings assertion
still expected the proxy tab disabled; it was corrected to assert proxy enabled
and other unsupported settings disabled. A fresh profile/final signed rebuild
then produced 82/82. No normal-profile smoke or user-app shutdown was performed.

Final `git diff --check` passed, and `git diff --exit-code -- go.mod go.sum
bun.lockb bun.lock` confirmed no dependency-file changes. Every delegated worker
and nested worker completed before integration handoff. Host task bookkeeping
and final independent review remain owned by the main session. The detailed
final message is also emitted through the coordinator's configured
`output-last-message` destination (`workers/providers-implementation.summary.md`).

## Explicit limits and remaining manual work

- Live Gmail refresh/revocation/reconnect, OS keyring lock/persistence, offline
  real-account restart and expanded To/Cc/Bcc/attachment delivery remain pending.
- Microsoft native client registration, personal/organizational consent, actual
  scope/expiry/revocation and tenant SMTP AUTH policy require dedicated accounts.
  No real credentials or live server requests were used for synthetic tests.
- SMTP acceptance and Graph 202 do not prove delivery. SMTP has no authoritative
  Sent lookup; inconclusive checks never authorize resend. Graph Sent checking
  settles only a unique complete exact match; absence stays unknown.
- IMAP IDLE watches the first selected folder; other selected folders poll.
  Moves require native MOVE or safe sequential UIDPLUS COPY/UID STORE/UID EXPUNGE;
  a server lacking both is rejected without global EXPUNGE. Sparse UID ranges
  can require many bounded continuation passes.
- POP3 bad/oversized messages abort the current pass while preserving old cache
  and checkpoints. No automatic deletion or invented remote flag support exists.
- Graph MIME send supports a 3 MiB raw budget for a 4 MiB base64 request; large
  attachment upload sessions are not advertised. Malformed hydration preserves
  the page boundary/readable old cache and reports failure.
- The historical seven packaged M2 assertions use Go application services and
  protocol servers in a separate runtime. Review adds seven distinct WebView
  assertions for IMAP setup/settings/read and SMTP/Bcc compose in the actual UI
  runtime. Its execution evidence is recorded separately in
  `m2-review-validation.md`. Broader platform runtime acceptance remains manual;
  cross-compilation alone is not Windows/Linux runtime evidence.
- The November 3 continuation adds English/Chinese UI resources for the new
  protocol, Microsoft, folder and routing panels, including accessibility labels,
  confirmation prompts and resource states. Backend error diagnostics retain
  their existing language. See `m2-review-validation.md` for current evidence.
- Isolation caveat: an early frontend typecheck ran before the no-env wrapper;
  the final local build's `bun install` also reported auto-loading `.env` despite
  the wrapper flag. No contents were printed or manually opened. OAuth build
  variables were explicitly empty. Subsequent build steps did not invoke install
  again. This is a recorded tool-isolation limitation, not a claim that the file
  was never automatically read.
