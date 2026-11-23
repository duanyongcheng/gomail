# GoMail

[![Go](https://img.shields.io/badge/Go-1.27.3-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![MyGO](https://img.shields.io/badge/MyGO-v0.6.4-4B5563)](https://github.com/egoist/mygo)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev/)

A local-first desktop email client built with **Go**, **MyGO WebView**, and **React**.

GoMail keeps your mail archive, full-text search index, HTML sanitizer, encrypted drafts, durable Outbox, and credentials entirely on your machine—without cloud middleware or browser webmail tabs.

- **Repository:** <https://github.com/duanyongcheng/gomail>
- **Issues:** <https://github.com/duanyongcheng/gomail/issues>
- **Releases:** <https://github.com/duanyongcheng/gomail/releases>

---

## Features

### Supported Mail Providers

- **Gmail (Google OAuth 2.0 PKCE):** Multi-account support with `gmail.modify` + `openid email profile`, resumable full-mailbox backfill and paginated History sync, message download failure persistence and retry queue (up to 50 MiB), durable FIFO remote-write queue, encrypted local drafts with explicit Gmail cloud-copy upload/check/update/import/delete and cloud-linked sending, encrypted durable Outbox with unknown-outcome protection, account-scoped conversations and reply threading, custom server label CRUD, per-account sender trust, offline account editing with encrypted signatures, per-account proxy/network routing, and account access states with safe same-mailbox reconnection. Permanent delete / empty-trash is disabled under `gmail.modify`.
- **IMAP & SMTP:** Account setup and editing with verified TLS/STARTTLS, explicit endpoint CA trust, and global/direct/custom HTTP CONNECT or SOCKS5 proxy routing. Selected IMAP folders maintain durable `UIDVALIDITY`-aware checkpoints, queued flags/moves/trash, server folder management (create/rename/delete), and scheduler-owned IDLE (first selected folder; additional selected folders poll). SMTP sends through the encrypted Outbox with frozen `Bcc` envelope recipients and `Bcc`-free MIME.
- **POP3:** Verified TLS/STLS, bounded `UIDL`/`RETR` parsing, `UIDL` checkpoints and deduplication, and deliberately disabled remote flag/folder mutations. Optional configured SMTP enables sending.
- **Microsoft / Outlook & Experimental Graph:** Native public-client OAuth 2.0 PKCE with validated tenant/audience policy, separate Graph/Exchange resource grants, and independent IMAP/SMTP XOAUTH2. Experimental Microsoft Graph supports recursive folder discovery, resumable delta/raw MIME sync, durable writes, and `sendMail` acceptance.

### Local-First Architecture, Privacy & Desktop Integration

- **Local-First & Offline Use:** Pure-Go SQLite (`modernc.org/sqlite`, `CGO_ENABLED=0`) with WAL mode, `synchronous=FULL` writer durability, FTS5 local search, unread counts, and RFC 2047 address display-name decoding. Cached mailboxes, search, drafts, and local account settings remain usable offline while remote actions expose explicit queued, retrying, or uncertain states.
- **Privacy HTML Renderer & Attachments:** Message HTML and CSS are sanitized in Go before reaching the WebView (`Strict`, `LoadOnce`, `Off`, `TrustedSender` modes) with tracking-pixel/domain reporting and backend-enforced per-account sender trust. Inline CID images are served via the narrow `gomail-attachment://` protocol. Attachments support **Download** (system Downloads directory) and **Save As** (native save dialog) with collision-safe filenames and symlink-contained cache reads.
- **Encrypted Credentials & Local Data:** OAuth tokens, protocol passwords, signatures, local drafts, Outbox snapshots, and in-app Google OAuth settings are encrypted with the GoMail v1 AES-256-GCM envelope (`GOMAIL\x00\x01`, AAD `gomail.crypto.context.v1`) using a 32-byte master DEK stored in the OS keyring (`dev.bary.gomail`). A locked keyring or missing DEK preserves existing ciphertext without plaintext fallback.
- **Desktop Hardening & Auto-Updates:** Native close-to-background vs. quit-on-close (`Settings → General → Quit app when window is closed`), single-instance window restoration, validated external HTTP(S) link opening, bilingual Chinese/English UI under the unified Harbor Chartroom design system (`light`, `dark`, `system` modes plus independent message-detail reading templates), and signed in-app auto-updates with bsdiff delta patches and read-only GitHub Releases fallback (**Settings → About** and macOS menu **Check for Updates…**).

### Guarded / Upcoming Capabilities

Views and settings tabs not yet backed by runtime services remain visibly guarded with truthful unavailable notices: advanced search filters, local labels, contacts, Kanban board, snooze, rules, templates, AI assistant, translation, WebDAV/file backup, desktop notifications/tray/autostart, hidden startup, and the in-app diagnostic log viewer.

---

## Getting Started

### Prerequisites

- **Go** `1.27.3`
- **Bun**
- **Node.js** `22+` (for Vite / Vitest)
- Platform WebView runtime support (macOS WebKit / Linux WebKitGTK / Windows WebView2)

### Clone & Run in Development

```sh
git clone https://github.com/duanyongcheng/gomail.git
cd gomail
bun install
bun run generate
bun run dev
```

An empty profile opens cleanly and stays running. Credential restoration on startup never contacts the network. Once the UI loads the profile's sync mode preference, configured accounts start syncing unless set to `manual`. Use **Settings → Accounts** to sign in or add accounts; manual sync can always be triggered explicitly.

---

## Building & Releases

### Local macOS Build

```sh
bun run build:local
# Or run the script directly from any working directory:
./scripts/build-local.sh
```

The script verifies `go`, `bun`, `node`, and `codesign`, installs dependencies with `--frozen-lockfile`, regenerates MyGO bindings, type-checks and builds the frontend, packages an ad-hoc signed app for the host Mac architecture (`CGO_ENABLED=0`), and verifies the signature with `codesign --verify --deep --strict`. OAuth credentials are **never** embedded into packaged binaries.

The output bundle is written to `build/darwin-<arch>/GoMail.app`. On Apple Silicon:

```sh
open "build/darwin-arm64/GoMail.app"
```

### Cross-Platform Builds & GitHub Releases

For DMGs or other platform targets locally, pass MyGO flags through `bun run build` (optionally setting `GOMAIL_VERSION`):

```sh
GOMAIL_VERSION=0.2.2 bun run build -skip-dmg -skip-notarize
```

GitHub Actions workflows live in `.github/workflows/`:

- **[ci.yml](.github/workflows/ci.yml):** Runs `bun run check` and verifies generated bindings (`src/mygo.ts`) on pushes and pull requests to `master`.
- **[release.yml](.github/workflows/release.yml):** Builds installers across **macOS** (`darwin/arm64`, `darwin/amd64` `.dmg`, ad-hoc signed), **Windows** (`windows/amd64`, `windows/arm64` NSIS `Setup .exe`), and **Linux** (`linux/amd64`, `linux/arm64` `.deb`, `.tar.gz`, and `install.sh`), sets `GOMAIL_VERSION` from the release tag or `mygo.config.ts`, signs update archives, bsdiff incremental deltas (for up to 3 previous signed versions), and per-platform `update-<target>.json` manifests using `MYGO_UPDATER_PRIVATE_KEY`, generates and verifies `SHA256SUMS.txt`, and publishes a GitHub Release on `v*` tag pushes or manual `workflow_dispatch`.

---

## OAuth Configuration

### In-App Google OAuth Configuration (Recommended)

Configure `GOOGLE_CLIENT_ID` and optional `GOOGLE_CLIENT_SECRET` directly inside GoMail via **Settings → Accounts → Configure Google OAuth** or inside the **Add Email Account** dialog:

- Credentials are encrypted with your profile's OS keyring DEK (`dev.bary.gomail`) and stored locally in SQLite (`google_oauth_settings`, never in plaintext).
- Changes apply **immediately without restarting**: Google sign-in unlocks right away, matching Gmail accounts restore automatically, secret-only updates hot-rebuild providers, and switching to a different `Client ID` preserves local mail/drafts while marking existing Gmail accounts for reconnection.
- Precedence: **In-app saved configuration > Process environment variables (`.env` in dev)**. Clearing in-app credentials immediately falls back to runtime environment variables (if set).

### Runtime Environment Variables (Development Only)

Copy `.env.example` to `.env` if you want local runtime defaults during `bun run dev`:

```dotenv
GOOGLE_CLIENT_ID=your-desktop-client-id.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=your-desktop-client-secret
```

- `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` are **never embedded** when packaging (`bun run build`, `./scripts/build-local.sh`, or GitHub Releases).
- Gmail requests `gmail.modify` plus `openid email profile` using PKCE and a one-shot loopback callback in the default system browser.
- Microsoft setup accepts a native public-client ID, validated tenant/audience, and separate resource grants directly in the account form (no client secret).

> **Security:** Never commit `.env`, OAuth credentials, tokens, profiles, or mailbox data to Git. `.env` and `.env.*` are gitignored; `.env.example` contains placeholders only.

---

## Profiles & Storage

GoMail's application identifier and OS keyring service name are `dev.bary.gomail`, with display name `GoMail`. Profile directory resolution follows this precedence:

1. `GOMAIL_PROFILE_DIR` environment variable (relative paths resolve beside the executable).
2. First line of `gomail-profile.txt` beside the executable.
3. A `gomail-profile/` directory beside the executable.
4. MyGO's platform user-data directory (`GoMail`).

Inside a profile directory:

- `profile-id`: persistent namespace isolating frontend preferences even if the profile directory is moved.
- `gomail.sqlite`: pure-Go SQLite database (1 writer connection with WAL + `synchronous=FULL` + immediate transactions, 4 read-only connections, foreign keys enabled, 15-second busy timeout).
- `attachments/`: cached attachment files referenced by relative `attachments.local_path`.
- `logs/gomail.log`: rotating JSON operational logs (up to 3 × 2 MiB files; operation statuses only—no credentials or mail payloads).

### SQLite Schema Summary (`v1`–`v33`)

All migrations are embedded in `internal/store/migrations/` and run transactionally with `PRAGMA user_version` tracking. Unversioned non-empty databases are rejected rather than silently overwritten.

- **Core Mail & Workspace (`v1`–`v20`):** Accounts (`gmail`, `imap`, `pop3`, `outlook`, encrypted `auth_data`, colors, sort order), folders, messages, message-folder mappings, attachments (with `content_id`), trusted senders, folder sync state, sync failures, `pending_mail_ops`, and reserved tables for upcoming local features.
- **Search, Sync & Durable Operations (`v21`–`v33`):**
  - `v21`: FTS5 full-text search index (`message_search`).
  - `v22`: Account-scoped Gmail backfill reconciliation table.
  - `v23`: Persistent message download failures and retry timing.
  - `v24`: Operation-sequence sidecar (`mail_op_sequence`) for FIFO replay and sync reconciliation.
  - `v25`: Encrypted local compose drafts (`compose_drafts`) and deduplicated staged attachments (excluded from remote sync and FTS).
  - `v26`: Encrypted durable send Outbox (`outbox`), independent of idempotent mail mutations.
  - `v27`: Account-scoped conversation index (`message_threads`).
  - `v28`: Unresolved Gmail label-management write log (`gmail_label_changes`).
  - `v29`: Encrypted account-scoped email signatures (`email_signatures`).
  - `v30`–`v31`: Gmail cloud draft copy mappings, write intents, deletion/send lifecycle states, and Outbox links.
  - `v32`: Provider/account/scope checkpoints (`provider_checkpoints`), remote identity mappings (`provider_mappings`), and encrypted global network/proxy settings (`network_settings`).
  - `v33`: Encrypted in-app Google OAuth client configuration (`google_oauth_settings`).

---

## Testing & Verification

### Quality Gate

```sh
bun run test
bun run check
```

`bun run check` executes the full verification suite:

1. `go test -count=1 ./...` and `go vet ./...` (`CGO_ENABLED=0`)
2. `go test -race -count=1 ./internal/...` (`CGO_ENABLED=1`)
3. `go tool mygo vet .` and `go tool mygo generate`
4. TypeScript type-checking (`bun run typecheck`), Vitest (`bun run test`), and Vite production build (`bun run build:web`)
5. CGO-free cross-compilation for `darwin/amd64`, `linux/amd64`, `linux/arm64`, `windows/amd64`, and `windows/arm64`

Temporary logs and build artifacts are cleaned up automatically on success; set `GOMAIL_CHECK_KEEP_ARTIFACTS=1` to preserve them for inspection.

### Isolated Desktop Selftest

GoMail includes a synthetic desktop selftest that runs against local mock Gmail and TLS IMAP/SMTP/POP3 fixtures using an isolated app name (`GoMail Selftest`), disposable profile, and in-memory keyring. It writes a JSON report and screenshots, then exits without contacting real providers or modifying the OS keyring:

```sh
CGO_ENABLED=0 go build -o /tmp/gomail-selftest .
GOMAIL_SELFTEST=1 \
GOMAIL_PROFILE_DIR=/tmp/gomail-test-profile \
GOMAIL_SELFTEST_DIR=/tmp/gomail-test-results \
/tmp/gomail-selftest
```

Always point `GOMAIL_PROFILE_DIR` to a disposable temporary directory when running the selftest.

---

## Documentation

- [docs/architecture.md](docs/architecture.md) — System architecture, `internal/` package map, storage/encryption, privacy renderer, sync/outbox/pending-ops pipelines, auto-updater, and development workflow.
- [PRODUCT.md](PRODUCT.md) — Product scope, users, principles, capabilities, and roadmap.
- [DESIGN.md](DESIGN.md) — Harbor Chartroom design system, tokens, typography, layout, and component rules.

---

## Built With

- [MyGO](https://github.com/egoist/mygo) — Go desktop WebView framework and signed updater.
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) — Pure-Go SQLite engine (`CGO_ENABLED=0`).
- [go-imap](https://github.com/emersion/go-imap), [go-smtp](https://github.com/emersion/go-smtp), [go-message](https://github.com/emersion/go-message) — Go mail protocol and MIME libraries.
- [React](https://react.dev/), [Vite](https://vite.dev/), and [TipTap](https://tiptap.dev/) — Frontend UI and rich-text/Markdown composer.
