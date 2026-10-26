# M0 validation

M0 S1–S6 is implemented. Real Google authorization remains a manual acceptance
step. No push, old-profile import or real mailbox operation was performed.

## Implemented

- `internal/store`: modernc SQLite, one writer/four read-only connections,
  transactional v1–v20 migration port and Go v21 FTS5 cache. The 44 legacy schema
  objects match the independent Rust golden. V12 account rebuild, V14/V16 related
  data merging, mailbox-scoped UIDs, V19/V20 resume, rollback and read concurrency
  are covered. Root `schema.sql` and startup ALTERs are removed.
- `internal/crypto`, `oauth`: Rust-compatible v1 envelope/AAD, independent keyring
  adapter, DB token CAS, environment/build client precedence, required Gmail
  scopes and lazy token restoration. Two expired accounts restore without HTTP
  or browser calls; fresh profiles do not call keyring.
- `domain`, `provider/gmail`: account-scoped message/folder identities, bounded
  recent cache and History sync, atomic cursor/cache/search, flag updates,
  account isolation and file-backed attachment content. Fetch/MIME failures
  preserve the previous cache/index/cursor. Shared REST/OAuth/MIME code was
  extracted so product core does not depend on MyGO.
- Typed Account/Folder/Message/Search/Sync/App services replace `Backend.Invoke`.
  The frontend keeps its upstream `api.ts` API through the explicit command
  table. All 145 invoke commands are documented in `parity.md` with implemented,
  startup-stub or action-stub status; unknown commands reject.
- Normal startup, persistent profile namespace, cancellation, log rotation,
  fixed `Pebble Go` identity, titlebar drag adaptation and explicit selftest.
  Product code no longer includes `GmailPanel`, harness or `pocasset` exception.
- Original 130 frontend test files copied; seven exclusions are individually
  justified in `tests/skipped.json`. Gmail prototype persistence is retained
  only as test fixtures. IMAP/SMTP remain isolated until M2.

## Checks performed

`bun run check` passed every step:

| Check | Result |
|---|---|
| CGO-free `go test -count=1 ./...` | Passed |
| `go vet ./...` | Passed |
| `go test -race -count=1 ./internal/...` | Passed |
| `go tool mygo vet .` | Passed |
| Binding generation | Passed |
| TypeScript | Passed |
| Vitest | 124 files, 715 tests passed |
| Vite production build | Passed |
| CGO-free darwin/amd64 build | Passed |
| CGO-free linux/amd64 and linux/arm64 builds | Passed |
| CGO-free windows/amd64 and windows/arm64 builds | Passed |

After the full gate, an additional provider MIME failure regression passed in
normal and race mode. UI title/capability changes are included in the final
frontend gate and packaged build.

The final macOS `.app` builds with `bun run build -skip-dmg -skip-notarize` and
passes `codesign --verify --deep --strict` (ad-hoc signing). Packaged local
selftest passes all four assertions: React inbox, persisted account,
provider/search and opaque cursor. A screenshot was captured and inspected.
A normal empty-profile packaged run stays alive, creates v21 with zero accounts,
and exits cleanly on SIGTERM. A build with a nonexistent profile override does
not create that directory, checking that codegen has no profile side effects.

The migration source SHA-256 still matches the Rust golden after verification:
`442a80d2c9dea502590b93e2fef1a991e8d9bdcea71c9ac1975dc0bbb630a619`.
The reference checkout's existing modifications were not changed by this work.

## Evidence from this session

- Full gate log: `/tmp/pebble-go-m0-check-final.log`.
- Detailed check logs and cross-compiled binaries:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-bGX46o/`.
- Final package build log: `/tmp/pebble-go-m0-package-final.log`.
- Normal startup report: `/tmp/pebble-go-m0-normal-report.json`.
- Packaged selftest JSON and screenshot:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-m0-final-3lfpqcy3/results/`.
- App bundle: `build/darwin-arm64/Pebble Go.app`.

Temporary evidence paths are machine-local and can be deleted by the OS. The
reproducible tests, golden, command parity and scripts are in the repository.

## Remaining acceptance and milestone boundaries

Real Google login/consent and an interactive real-account restart/sync still
require the user. The automated token-restoration/provider tests use synthetic
accounts and local services. Windows/Linux results are compilation checks.

M1 retains full backfill, scheduling, privacy HTML/CID renderer, durable ops and
outbox, send/drafts, labels CRUD and attachment export. Current Gmail flag
changes preserve the existing slice capability and do not implement the M1
persistent mutation queue. Automatic sync reasons stay disabled during M0.
M2 connects IMAP/SMTP; M6 owns old-profile/keyring migration and release signing.
