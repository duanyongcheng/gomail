# Pebble Go

[![Go](https://img.shields.io/badge/Go-1.27.3-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![MyGO](https://img.shields.io/badge/MyGO-v0.6.4-4B5563)](https://github.com/egoist/mygo)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev/)

A local-first desktop email client built with **Go**, **MyGO WebView**, and **React**.

Pebble Go is an independent Go + MyGO implementation of [Pebble](https://github.com/QingJ01/Pebble), reusing the Pebble React UI while keeping the backend, storage, cryptography, and packaging completely separate from the upstream Rust/Tauri implementation.

- **Repository:** <https://github.com/duanyongcheng/pebble-go>
- **Issues:** <https://github.com/duanyongcheng/pebble-go/issues>
- **Releases:** <https://github.com/duanyongcheng/pebble-go/releases>

---

## Features & Current Status

### Supported Mail Providers (M0–M2)

- **Gmail (Google OAuth PKCE):** Multi-account support with `gmail.modify` + `openid email profile`, resumable full-mailbox backfill and paginated History sync, large-message download failure persistence and retry queue (up to 50 MiB), durable FIFO remote-write queue, encrypted local drafts with explicit Gmail cloud-copy upload/check/update/import/delete and cloud-linked sending, encrypted durable Outbox with unknown-outcome protection, account-scoped conversations and reply threading, custom server label CRUD, per-account sender trust, offline account editing with encrypted signatures, per-account proxy/network routing, and account access states with same-mailbox reconnection. Permanent delete / empty-trash remains unavailable under `gmail.modify`. See [docs/m1-gmail-completion.md](docs/m1-gmail-completion.md).
- **IMAP & SMTP:** Production account setup and editing with verified TLS/STARTTLS, explicit endpoint CA trust, and global/direct/custom HTTP CONNECT or SOCKS5 proxy routing. Selected IMAP folders use durable `UIDVALIDITY`-aware checkpoints, queued flags/moves/trash, server folder management (create/rename/delete), and scheduler-owned IDLE (first selected folder; other selected folders poll). SMTP uses the encrypted Outbox with frozen `Bcc` envelope recipients and `Bcc`-free MIME; server acceptance may omit a remote ID and does not prove recipient delivery.
- **POP3:** Verified TLS/STLS, bounded `UIDL`/`RETR` parsing, `UIDL` checkpoints and deduplication, and deliberately disabled remote flag/folder changes. Optional configured SMTP enables sending.
- **Microsoft / Outlook & Experimental Graph:** Native public-client OAuth PKCE with validated tenant/audience policy, separate Graph/Exchange resource grants, and independent IMAP/SMTP XOAUTH2. Experimental Microsoft Graph supports recursive folder discovery, resumable delta/raw MIME sync, durable writes, and `sendMail` 202 acceptance. See [docs/m2-integration-validation.md](docs/m2-integration-validation.md).

### Local-First Workflows, Privacy & Hardening

- **Local-First & Offline Use:** Pure-Go SQLite (`modernc.org/sqlite`, `CGO_ENABLED=0`) with WAL and `synchronous=FULL` writer durability, FTS5 local search, unread counts, and RFC 2047 address display-name decoding (including quoted encoded names across new and cached mail; see [docs/address-name-decoding-validation.md](docs/address-name-decoding-validation.md)). Supported local views and durable acknowledgement-first actions remain usable offline while remote actions keep explicit network boundaries (see [docs/m1-offline-local-workflows-validation.md](docs/m1-offline-local-workflows-validation.md)).
- **Privacy Renderer & Attachments:** Message HTML is sanitized in the Go core before reaching the WebView (`Strict`, `LoadOnce`, `Off`, `TrustedSender` modes) with tracker reporting and backend-enforced per-account sender trust ([docs/m1-privacy.md](docs/m1-privacy.md), [docs/m1-trusted-senders.md](docs/m1-trusted-senders.md)). Inline CID images use the narrow `pebble-attachment://` protocol. Attachments support **Download** (system Downloads directory) and **Save As** (native file chooser) with numbered collision avoidance and symlink-contained cache reads ([docs/m1-attachment-export.md](docs/m1-attachment-export.md)).
- **Desktop Hardening:** Native close-to-background vs. quit-on-close (`Settings → General → Quit app when window is closed`) with single-instance window/compose state restoration, validated external HTTP(S) link opening, and shared unavailable-feature guards ([docs/m1-daily-use-hardening.md](docs/m1-daily-use-hardening.md)).

### Not Yet Available & Known Limits

- **Guarded / Unimplemented UI Features:** Advanced search filters, local labels, contacts, Kanban board, snooze, rules, templates, AI assistant, translation, WebDAV/file backup, desktop notifications/tray/autostart, hidden startup, diagnostic log viewer, and in-app update checks are disabled or hidden until implemented.
- **Legacy Profile Import:** Importing live profiles from the Rust/Tauri Pebble app remains planned M6 work; use a separate Pebble Go profile.
- **Acceptance Scope:** Automated gates and packaged macOS `arm64` synthetic selftests pass, and cross-compilation covers macOS, Linux, and Windows targets, which does not substitute for Windows/Linux runtime validation or live-account acceptance. Ongoing live-account acceptance records and deferred items are tracked in [docs/m1-m2-live-acceptance.md](docs/m1-m2-live-acceptance.md).

---

## Getting Started

### Prerequisites

- **Go** `1.27.3`
- **Bun**
- **Node.js** (for Vite / Vitest)
- Platform WebView runtime support (macOS WebKit / Linux WebKitGTK / Windows WebView2)

### Clone & Run in Development

```sh
git clone https://github.com/duanyongcheng/pebble-go.git
cd pebble-go
bun install
bun run generate
bun run dev
```

An empty profile opens normally and stays running. Token restoration never contacts the network. Once the UI loads the profile's realtime preference, configured accounts start syncing unless the preference is `manual`. Use **Settings → Accounts** to sign in or add accounts; manual sync can always be triggered explicitly.

---

## Building

### Local macOS Build

```sh
bun run build:local
# Or run the script directly from any working directory:
./scripts/build-local.sh
```

The script verifies `go`, `bun`, `node`, and `codesign`, installs dependencies with the frozen Bun lockfile, regenerates MyGO bindings, type-checks and builds the frontend, and packages an ad-hoc signed app for the current Mac architecture. OAuth credentials are **never** embedded into packaged binaries. DMG creation and notarization are skipped, and the bundle signature is verified with `codesign --verify --deep --strict`.

The output bundle is written to `build/darwin-<arch>/Pebble Go.app` (replacing the target directory after a successful build so stale DMGs are removed). For Apple Silicon:

```sh
open "build/darwin-arm64/Pebble Go.app"
```

### Other Targets & GitHub Releases Packaging

For a DMG or other platform targets locally, run `bun run build` with MyGO flags, for example:

```sh
bun run build -skip-dmg -skip-notarize
```

GitHub Actions workflows are included in `.github/workflows/`:

- **[ci.yml](.github/workflows/ci.yml):** Runs `bun run check` and verifies generated bindings (`src/mygo.ts`) on pushes and pull requests to `master`.
- **[release.yml](.github/workflows/release.yml):** Builds installers across **macOS** (`darwin/arm64`, `darwin/amd64` `.dmg`, ad-hoc signed), **Windows** (`windows/amd64`, `windows/arm64` NSIS `Setup .exe`), and **Linux** (`linux/amd64`, `linux/arm64` `.deb`, `.tar.gz`, and `install.sh`), signs update archives, bsdiff incremental delta patches (for up to the 3 previous signed versions), and per-platform `update-<target>.json` manifests using `MYGO_UPDATER_PRIVATE_KEY`, generates `SHA256SUMS.txt`, and publishes a GitHub Release when pushing a `v*` tag (e.g. `git tag v0.2.1 && git push origin v0.2.1`) or manually triggering via `workflow_dispatch`. No `GOOGLE_CLIENT_ID` or `GOOGLE_CLIENT_SECRET` secrets are required or embedded in release builds.

---

## OAuth Configuration

### In-App Google OAuth Configuration (Recommended)

You can configure `GOOGLE_CLIENT_ID` and optional `GOOGLE_CLIENT_SECRET` directly inside the app via **Settings → Accounts → Configure Google OAuth** or inside the **Add Email Account** dialog:

- Credentials are encrypted with your profile's OS keyring DEK (`dev.bary.pebble.go`) and stored locally in SQLite (`google_oauth_settings`, never in plaintext).
- Changes take effect **immediately without restarting**: the Google sign-in button unlocks right away, matching Gmail accounts restore automatically, secret-only updates hot-rebuild Gmail providers, and switching to a different `Client ID` preserves all local mail/drafts while marking existing Gmail accounts for reconnection.
- Precedence: **In-app saved configuration > Process environment variables (`.env` in dev)**. Clearing the in-app configuration immediately falls back to runtime environment variables (if any).

### Runtime Environment Variables (Development Only)

Copy `.env.example` to `.env` only if you want runtime defaults during local `bun run dev` development:

```dotenv
GOOGLE_CLIENT_ID=your-desktop-client-id.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=your-desktop-client-secret
```

- `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` are **never embedded** when packaging (`bun run build`, `./scripts/build-local.sh`, or GitHub Releases).
- Without a configured Google Client ID, Google sign-in remains disabled until configured in-app or via runtime environment variables.
- Gmail requests `gmail.modify` plus `openid email profile` using PKCE and a one-shot loopback callback in the system browser. User consent and real Google account verification are manual steps; automated checks use local fixtures only.
- Microsoft setup accepts a native public-client ID, validated tenant/audience, and separate resource consent directly in the account form; no Microsoft client secret is used.

OAuth tokens and protocol credentials are encrypted in `accounts.auth_data` using the Pebble v1 AES-256-GCM envelope bound to account identity. The master DEK is stored in the independent `dev.bary.pebble.go` OS keyring service, and token refresh uses compare-and-swap persistence. A locked keyring or missing DEK preserves ciphertext and requires attention with no plaintext fallback; the upstream Pebble keyring is never read.

> **Security:** Never commit `.env`, OAuth credentials, tokens, profiles, or mailbox data to Git. `.env` and `.env.*` are gitignored; `.env.example` contains placeholders only.

---

## Profiles & Storage

The application identifier is `dev.bary.pebble.go` and its display name is `Pebble Go`. Profile directory resolution follows this precedence:

1. `PEBBLE_PROFILE_DIR` (relative paths resolve beside the executable).
2. First line of `pebble-profile.txt` beside the executable.
3. A `pebble-profile` directory beside the executable.
4. MyGO's platform user-data directory.

Inside a profile directory:

- `profile-id`: persistent namespace isolating frontend preferences even if the profile is moved.
- `pebble.sqlite`: SQLite database (`database/sql` + `modernc.org/sqlite`, 1 writer + 4 read-only connections, WAL, foreign keys, 15-second busy timeout, and `synchronous=FULL` on every writer connection including pool replacement).
- `attachments/`: cached attachment content referenced by relative `attachments.local_path`.
- `logs/`: rotating operational logs (operation statuses only — no credentials or mail payloads).

### Schema Migrations

- **v1–v20:** Match the upstream Pebble Rust schema.
- **v21–v33 (Go additions without altering v1–v20 tables):**
  - `v21`: FTS5 search cache.
  - `v22`: Account-scoped backfill reconciliation table.
  - `v23`: Persistent Gmail message download failures and retry timing.
  - `v24`: Operation-sequence sidecar for FIFO replay and sync reconciliation.
  - `v25`: Separate encrypted local drafts, staged attachment objects, and reference tracking (excluded from remote sync and FTS).
  - `v26`: Encrypted send Outbox, independent of idempotent mail mutations.
  - `v27`: Account-scoped conversation index.
  - `v28`: Unresolved Gmail label-management write log for safe reconciliation.
  - `v29`: Encrypted account-scoped Gmail signatures.
  - `v30`: Gmail cloud-copy identities and durable write intents.
  - `v31`: Cloud-copy deletion/send lifecycle states and Outbox association.
  - `v32`: Provider/account/scope checkpoints, remote identity mappings, and explicit global network configuration.
  - `v33`: Encrypted in-app Google OAuth client configuration.

Unversioned PoC databases are rejected rather than silently rewritten.

---

## Sync, Compose & Operational Contracts

- **Sync & Scheduling ([docs/m1-sync.md](docs/m1-sync.md)):** Backfill runs in pages of up to 100 messages, persisting checkpoints per page (up to 10 pages per pass; unfinished work continues after 1 second, including manual backfill). Realtime polling uses 10s active / 30s idle / 120s background with backoff up to 5 minutes; balanced and battery modes use 30s and 60s foreground intervals; manual mode disables scheduled polling and ignores focus/reconnect triggers.
- **Download Recovery ([docs/m1-recovery.md](docs/m1-recovery.md)):** Gmail raw and decoded MIME content is bounded at 50 MiB. Malformed or oversized messages are recorded for retry without blocking other mail. Inspect and retry failures under **Settings → Accounts → Mail downloads**.
- **Remote-Write Queue ([docs/m1-remote-ops.md](docs/m1-remote-ops.md), [docs/m1-remote-ops-reliability.md](docs/m1-remote-ops-reliability.md)):** Flags, archive/restore, trash, moves, batch actions, and mark-all-read commit locally and enqueue a remote write atomically. Inspect pending retries and dismiss failures in **Settings → Remote Writes**. Unknown outcomes are never auto-retried.
- **Drafts & Cloud Copies ([docs/m1-compose-drafts.md](docs/m1-compose-drafts.md), [docs/m1-gmail-completion.md](docs/m1-gmail-completion.md)):** Compose autosaves locally every 3 seconds with encrypted body and staged attachments (zero network on autosave). Open blank compose → **Local drafts** for explicit **Upload to Gmail**, **Update Gmail copy**, read-only **Check Gmail copy**, cloud draft browsing/import, and conflict recovery/deletion. Lossy HTML-to-rich/Markdown conversions are refused without altering the source.
- **Durable Outbox ([docs/m1-gmail-outbox.md](docs/m1-gmail-outbox.md)):** Sending persists an encrypted MIME snapshot before submission, checks the primary mailbox, and separates acceptance from failure or unknown outcomes. Unknown sends are never auto-resent; use **Check Sent** in Compose → Outbox or Settings → Remote Writes.
- **Conversations & Threading ([docs/m1-threads.md](docs/m1-threads.md)):** Paginated thread lists with account-scoped Gmail thread IDs; header fallback never merges by subject alone.
- **Gmail Labels, Access & Settings ([docs/m1-gmail-labels.md](docs/m1-gmail-labels.md), [docs/m1-account-reauth.md](docs/m1-account-reauth.md), [docs/m1-account-settings.md](docs/m1-account-settings.md)):** Manage custom server labels online with system-label protection, inspect account access states and safely reconnect the same mailbox without losing cached mail/drafts/unknown sends, and edit sender name, label, color, and encrypted signature atomically offline.

---

## Checks & Local Desktop Selftest

```sh
bun run test
bun run check
bun run build -skip-dmg -skip-notarize
```

`bun run check` executes:

1. `go test -count=1 ./...` and `go vet ./...` (`CGO_ENABLED=0`)
2. `go test -race -count=1 ./internal/...` (`CGO_ENABLED=1`)
3. `go tool mygo vet .` and `go tool mygo generate`
4. TypeScript type-checking (`bun run typecheck`), Vitest (`bun run test`), and Vite production build (`bun run build:web`)
5. CGO-free cross-compilation for `darwin/amd64`, `linux/amd64`, `linux/arm64`, `windows/amd64`, and `windows/arm64`

Detailed logs and compile artifacts are written to a printed temporary directory. Cross-compilation does not claim Windows/Linux runtime validation.

All 130 upstream test files are present alongside Pebble Go adapter and feature tests. Seven upstream-only suites (six Rust/Tauri config suites and the separate public website suite) are explicitly excluded with individual reasons in [tests/skipped.json](tests/skipped.json).

### Explicit Desktop Selftest

The desktop selftest runs against synthetic local Gmail and TLS IMAP/SMTP/POP3 fixtures using an isolated application name (`Pebble Go Selftest`), disposable profile, and in-memory test keyring. It writes a JSON report and screenshots, then exits without contacting real mail providers or the OS keyring:

```sh
CGO_ENABLED=0 go build -o /tmp/pebble-go-selftest .
PEBBLE_SELFTEST=1 \
PEBBLE_PROFILE_DIR=/tmp/pebble-go-test-profile \
PEBBLE_SELFTEST_DIR=/tmp/pebble-go-test-results \
/tmp/pebble-go-selftest
```

Always use a dedicated temporary profile for selftest. Without `PEBBLE_SELFTEST=1`, the fixtures and automatic exit are disabled. Synthetic selftest evidence and limits are documented in [docs/m2-review-validation.md](docs/m2-review-validation.md); none of these checks establishes live-account acceptance.

---

## Architecture & Compatibility

```text
React UI (src/lib/api.ts)
  -> Explicit typed command table (src/lib/platform/)
  -> Generated MyGO bindings (src/mygo.ts)
  -> Application services (internal/app)
  -> Provider interfaces & domain models (internal/domain)
  -> Providers (internal/provider/{gmail,imap,smtp,pop3,graph})
  -> Storage, crypto & OAuth (internal/{store,crypto,oauth})
```

There is no generic backend command dispatcher. Every one of the 145 frontend invoke commands is tracked in [docs/parity.md](docs/parity.md): implemented commands call generated MyGO services, optional startup stubs return explicit safe values, unimplemented action stubs throw `NotImplemented` with the command name, and unknown commands fail.

### Regenerating the Rust v20 Schema Oracle

To regenerate `internal/store/testdata/legacy_v20.json` from a local checkout of the upstream Rust [Pebble](https://github.com/QingJ01/Pebble) repository without modifying it or opening user profiles:

```sh
python3 scripts/generate_legacy_golden.py /path/to/Pebble
```

---

## Documentation

| Area | Document |
| --- | --- |
| Frontend command parity | [docs/parity.md](docs/parity.md) |
| Gmail completion & draft lifecycle | [docs/m1-gmail-completion.md](docs/m1-gmail-completion.md) |
| Daily-use hardening contract | [docs/m1-daily-use-hardening.md](docs/m1-daily-use-hardening.md) |
| Offline local workflow validation | [docs/m1-offline-local-workflows-validation.md](docs/m1-offline-local-workflows-validation.md) |
| Address display-name decoding | [docs/address-name-decoding-validation.md](docs/address-name-decoding-validation.md) |
| M2 integration evidence & limits | [docs/m2-integration-validation.md](docs/m2-integration-validation.md) |
| M2 independent review & WebView selftest | [docs/m2-review-validation.md](docs/m2-review-validation.md) |
| M1/M2 live-account acceptance tracker | [docs/m1-m2-live-acceptance.md](docs/m1-m2-live-acceptance.md) |
| Historical Gmail PoC notes | [POC-README.md](POC-README.md) |

---

## Acknowledgements

- [Pebble](https://github.com/QingJ01/Pebble) — upstream Rust/Tauri desktop email client and React UI design.
- [MyGO](https://github.com/egoist/mygo) — Go desktop WebView framework.
