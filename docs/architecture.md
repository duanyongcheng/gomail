# GoMail Architecture

GoMail is a local-first desktop email client built with **Go** (`CGO_ENABLED=0`), **MyGO WebView**, and **React 19**. Business logic, cryptography, HTML sanitization, protocol sync, and SQLite storage live in framework-independent Go packages under `internal/`, while `main.go` and `internal/desktop` adapt them to the desktop WebView shell.

---

## System Overview

```text
React UI (src/)
  ├── Generated MyGO bindings (src/mygo.ts) + typed platform layer (src/lib/platform/)
  └── 13 push events (mail:*, attachment:*, app:*)
        │
        ▼
Desktop Shell & Bound Services (main.go, internal/desktop, internal/app)
  ├── 13 core services (Account, Microsoft, Folder, GmailLabel, Trust, Message,
  │                     Thread, Search, Sync, PendingOps, Attachment, Compose, Outbox)
  └── 3 desktop services (FileService, AppService, UpdateService)
        │
        ▼
Domain, Providers & Local Core (internal/{domain,provider/*,privacy,mailmime,mailnet,oauth,store,crypto,profile})
  ├── SQLite (gomail.sqlite: 1 WAL/FULL writer + 4 read-only readers, FTS5)
  ├── OS Keyring (dev.bary.gomail / master-dek) + AES-256-GCM envelope
  └── Custom WebView protocol (gomail-attachment://)
```

### Process Startup & Shutdown (`main.go`)

1. Sets app identity (`GoMail`, or `GoMail Selftest` when `GOMAIL_SELFTEST=1`) and acquires the single-instance lock.
2. Resolves the profile directory (`internal/profile`), creates/loads `profile-id`, and opens the rotating JSON log (`logs/gomail.log`).
3. Opens `gomail.sqlite` (`internal/store.Open`), runs transactional migrations through `v33`, and registers the `gomail-attachment://` protocol handler.
4. Calls `runtime.Restore()` to decrypt local account metadata and build lazy token sources—**never** contacting the network on startup.
5. Starts background durable workers (`RunPendingOpsWorker`, `RunOutboxWorker`) and opens the main window (`1200×850`, min `800×600`, hidden titlebar).
6. Locks down WebView navigation: `SetWindowOpenHandler` returns `nil` and `OnWillNavigate` calls `PreventDefault()` so untrusted email links can never navigate the SPA away; links open only through `AppService.OpenExternal`.
7. On quit, `mygo.App.Run()` exits first, then `main.go` cancels the root context, joins all background goroutines (`workers.Wait()`), closes the scheduler, closes SQLite, and closes the log file.

### Frontend–Backend Contract

- `mygo.Bind(...)` exposes 16 Go services to TypeScript via `go tool mygo generate` (`src/mygo.ts`).
- Backend state changes broadcast through 13 typed MyGO events:
  - Mail & sync: `mail:sync-complete`, `mail:sync-progress`, `mail:new`, `mail:realtime-status`, `mail:pending-ops-changed`, `mail:outbox-changed`, `mail:folders-changed`, `mail:account-changed`, `mail:trusted-senders-changed`
  - Attachments: `attachment:download-progress`
  - Auto-updates: `app:update-progress`, `app:update-status`, `app:open-updates`
- `src/lib/platform/commands.ts`, `src/lib/platform/manifest.ts`, and `src/lib/availability.ts` enforce explicit feature availability: implemented commands call generated MyGO bindings, unimplemented views receive safe startup defaults, and unavailable actions fail cleanly without silent no-ops.

---

## Package Map (`internal/`)

| Package | Responsibility |
| --- | --- |
| `internal/app` | `Runtime`, per-account `Scheduler`, `PendingOps` and `Outbox` workers, encrypted draft/cloud-draft orchestration, and 13 MyGO-bound service facades. |
| `internal/crypto` | GoMail v1 AES-256-GCM envelope (`GOMAIL\x00\x01`), context-bound AAD (`gomail.crypto.context.v1`), and OS keyring DEK loading (`dev.bary.gomail` / `master-dek`). |
| `internal/desktop` | MyGO adapters: OS keyring bridge, UI-thread window `Lifecycle`, validated external URL opener, `gomail-attachment://` handler, native attachment save dialog (`FileService`), rotating logs (`logs/gomail.log`), and signed auto-updater (`UpdateService`). |
| `internal/domain` | Framework-independent models (`Account`, `Message`, `Folder`, `Attachment`, `SyncProgress`, `NewMail`), `Transport` provider interfaces, access states, and capability flags. |
| `internal/googleapi` | Gmail REST API client, retry/backoff logic, History/list sync helpers, drafts, labels, and raw/cloud-draft sending. |
| `internal/mailcore` | Low-level IMAP/SMTP/MIME protocol helpers and bounded stream handling. |
| `internal/mailmime` | RFC 2047 display-name decoding (including quoted encoded words), MIME parsing, outgoing MIME construction, and thread header resolution. |
| `internal/mailnet` | Verified TLS/STARTTLS dialing, custom root CA PEM trust, and global/account/direct HTTP CONNECT & SOCKS5 proxy resolution. |
| `internal/oauth` | Google and Microsoft OAuth 2.0 PKCE loopback authorization, CAS-backed token vault, and runtime env config helpers. |
| `internal/privacy` | Pure-Go HTML/CSS sanitizer (`Strict`, `LoadOnce`, `Off`, `TrustedSender`), tracker pixel/domain detection, and `cid:` → `gomail-attachment://` rewriting. |
| `internal/profile` | Profile directory resolution and stable FNV-1a `profile-id` namespace generation. |
| `internal/provider/gmail` | Gmail provider: resumable backfill, paginated History sync, 50 MiB bounded download recovery, label CRUD, cloud drafts, and durable writes/sending. |
| `internal/provider/imap` | IMAP provider: `UIDVALIDITY`-aware folder checkpoints, queued flag/move/trash replay, server folder CRUD, and scheduler-owned IDLE. |
| `internal/provider/smtp` | SMTP sender with frozen `Bcc` envelope recipients and `Bcc`-free MIME headers. |
| `internal/provider/pop3` | POP3 receiver with verified TLS/STLS, bounded `UIDL`/`RETR` parsing, and `UIDL` deduplication (remote mutations disabled). |
| `internal/provider/graph` | Experimental Microsoft Graph provider: recursive folder discovery, resumable delta/raw MIME sync, durable writes, and `sendMail`. |
| `internal/store` | Pure-Go SQLite (`modernc.org/sqlite`) writer/reader pools, embedded migrations (`v1`–`v33`), FTS5 search, and repository methods. |
| `internal/gmail`, `internal/testgmail`, `internal/testmail` | Synthetic mock Gmail HTTP server, local TLS IMAP/SMTP/POP3 test fixtures, and compatibility/regression test suites. |

---

## Profiles, SQLite Storage, Encryption & Keyring

### Profile Layout & Resolution

`internal/profile.Resolve` determines the active profile directory in order:

1. `GOMAIL_PROFILE_DIR` (relative paths resolve beside the executable).
2. First line of `gomail-profile.txt` beside the executable.
3. `gomail-profile/` directory beside the executable.
4. Platform user-data directory returned by `mygo.App.Path(mygo.PathUserData)` (`GoMail`).

Each profile contains:

- `profile-id` (`0600`): 16-byte random hex ID hashed into a stable namespace so frontend preferences stay bound to the profile even if moved.
- `gomail.sqlite` (`0600`): profile SQLite database.
- `attachments/`: cached attachment blobs addressed by relative path.
- `logs/gomail.log` (`0600`): JSON operational logs rotating at 2 MiB with up to 3 retained files (`gomail.log`, `.1`, `.2`). Credentials, URLs, and mail payloads are never logged.

### SQLite Pool & Durability (`internal/store`)

GoMail uses pure-Go `modernc.org/sqlite` (`CGO_ENABLED=0`) with split connection pools:

- **Writer pool (1 connection):** `mode=rwc`, `journal_mode=WAL`, `synchronous=FULL` (declared in the DSN so any replacement connection retains full durability), `_txlock=immediate`, `foreign_keys=ON`, `busy_timeout=15000`.
- **Reader pool (4 connections):** `mode=ro`, `query_only=ON`, `synchronous=NORMAL`, `foreign_keys=ON`, `busy_timeout=15000`.

Migrations `v1`–`v33` are embedded via `//go:embed migrations/*.sql` and execute transactionally using `PRAGMA user_version`. Unversioned non-empty SQLite files are rejected so foreign databases are never modified silently. Token refresh writes use `CompareAndSwapAuthData` to prevent concurrent refreshers from clobbering fresher tokens.

### Encryption & OS Keyring (`internal/crypto`)

- **Master DEK:** A 32-byte random key stored in the OS keyring under service `dev.bary.gomail`, account `master-dek`.
- **Missing-key safety:** `crypto.Load` generates a new DEK only when no encrypted records exist in the profile. If encrypted records already exist and the keyring is locked or missing the DEK, startup preserves all ciphertext, keeps cached mail readable, and prompts the user rather than overwriting keys or falling back to plaintext.
- **Envelope format:** GoMail v1 AES-256-GCM:
  ```text
  "GOMAIL\x00\x01" || nonce (12 bytes) || ciphertext || GCM tag (16 bytes)
  ```
- **Context-bound AAD:** Every encryption binds its ciphertext to both a logical purpose and record identifier:
  ```text
  "GOMAIL\x00\x01" || "gomail.crypto.context.v1\x00" || uint64_be(len(purpose)) || purpose || uint64_be(len(record)) || record
  ```
  Encrypted records include account credentials (`accounts.auth_data`), local compose drafts (`compose_drafts`), Outbox payloads/summaries (`outbox`), email signatures (`email_signatures`), global proxy settings (`network_settings`), and in-app Google OAuth credentials (`google_oauth_settings`).

---

## Privacy HTML Renderer & `gomail-attachment://`

### Go-Side HTML & CSS Sanitization (`internal/privacy`)

Untrusted message HTML is parsed and rewritten in Go (`golang.org/x/net/html`) before being sent to the WebView:

- **Element & attribute filtering:** Strips active/embedded elements (`script`, `iframe`, `object`, `embed`, `form`, `input`, `button`, `svg`, `canvas`, `meta`, `base`, `template`), removes all `on*` event attributes, and retains only an explicit allow-list of safe HTML tags, attributes, and `http`/`https`/`mailto` links (`target="_blank" rel="noopener noreferrer"`).
- **CSS sanitization:** Filters inline `style` attributes and `<style>` blocks (when permitted by mode) against an allow-list of safe properties, rejecting `url(`, `image-set(`, `expression(`, `javascript:`, `vbscript:`, `data:`, `@import`, and backslash escapes.
- **Privacy modes:**
  - `Strict` (default): blocks remote images, trackers, and `<style>` blocks; replaces blocked remote images with safe alt placeholders.
  - `LoadOnce`: permits remote non-tracker images and sanitized `<style>` blocks for a single view while still blocking known trackers and 1×1 tracking pixels.
  - `TrustedSender`: backend-verified per-account sender trust allowing remote images and sanitized `<style>` blocks.
  - `Off`: allows remote images, sanitized styles, and validated `http(s)` stylesheet links.
- **Plain-text fallback:** Messages without HTML (or failing HTML parsing) render escaped text inside `<pre class="gomail-plain-text-email">`.

### Inline Attachments (`gomail-attachment://`)

During sanitization, `cid:` image sources are matched against the message's stored attachments and rewritten to opaque URLs:

```text
gomail-attachment://localhost/<base64url(accountID + "\x00" + messageID + "\x00" + attachmentID)>
```

`internal/desktop/attachments.go` registers the `gomail-attachment` custom protocol handler, which:

1. Accepts only `GET` and `HEAD`.
2. Decodes `(accountID, messageID, attachmentID)` via `privacy.DecodeAttachmentURL`.
3. Opens the cached file through `db.OpenAttachment` (enforcing account/message ownership and symlink-free containment inside the profile's `attachments/` directory).
4. Streams regular files with their stored MIME type and `Cache-Control: no-store`.

---

## Durable Outbox, Pending Remote Writes & Sync Scheduler

### Pending Remote-Write Queue (`internal/app/pending_ops.go`)

- User actions (read/unread, star/unstar, archive/restore, trash, move, batch up to 200 messages, mark-all-read) update the local SQLite state and insert a `pending_mail_ops` entry with a monotonic `mail_op_sequence` number in a single `synchronous=FULL` transaction.
- `RunPendingOpsWorker` wakes on enqueue and replays operations FIFO per account against the account's `domain.Transport`.
- Sync reconciliation respects queued operations so incoming sync responses do not clobber unacknowledged local edits.
- Operations with uncertain network outcomes are surfaced for user inspection in **Settings → Remote Writes** and are never blindly re-executed.

### Durable Send Outbox (`internal/app/outbox.go`, `outbox_cloud.go`)

- `OutboxService.Queue` validates the message, computes a SHA-256 idempotency key from `ClientKey`, renders the outgoing MIME message (freezing SMTP `Bcc` recipients in envelope metadata while keeping MIME headers `Bcc`-free), encrypts the payload and summary, and persists the Outbox row before returning.
- `RunOutboxWorker` dispatches queued items via Gmail (`messages.send` or cloud-draft send), SMTP, or Microsoft Graph (`sendMail`).
- Delivery outcomes distinguish confirmed acceptance, retryable transport errors, and **unknown outcomes** (such as timeouts after transmit). Unknown sends are never auto-resent; users inspect or reconcile them via **Check Sent** in Compose → Outbox or Settings → Remote Writes.

### Sync Scheduler (`internal/app/scheduler.go`)

- Runs one cancellable goroutine worker per configured account with single-flight deduplication and clean join-on-shutdown semantics.
- Sync modes:
  - `realtime`: `10s` foreground active, `30s` foreground idle (after 1 minute without activity), `120s` background.
  - `balanced`: `30s` foreground active.
  - `battery`: `60s` foreground active.
  - `manual`: disables scheduled polling and focus/reconnect auto-triggers.
- Failures back off exponentially up to 5 minutes (`300s`).
- **Provider-specific behaviors:**
  - **Gmail:** Resumable backfill in pages of up to 100 messages (checkpointed per page; unfinished passes continue after 1s) followed by paginated History sync. Messages exceeding 50 MiB or failing MIME decode are recorded in `gmail_message_failures` for isolated retry without blocking the mailbox.
  - **IMAP:** Durable `UIDVALIDITY`-aware checkpoints per selected folder plus scheduler-owned IDLE on the first selected folder (additional selected folders poll).
  - **POP3:** `UIDL`-based deduplication checkpoints.
  - **Microsoft Graph (experimental):** Resumable folder/message delta sync with raw MIME fetches.

---

## Desktop Lifecycle & Auto-Updater

### Window Lifecycle (`internal/desktop/lifecycle.go`)

- `Lifecycle` serializes all window visibility and quit transitions onto the UI thread (`mygo.RunOnMain`).
- Before frontend preferences hydrate, closing the window defaults to hiding in the background; once hydrated, `ClosePreference` applies the profile's `keepRunningInBackground` setting.
- Explicit Quit (`Cmd+Q`, app menu, OS shutdown) calls `BeforeQuit()` so the next close destroys the window and triggers clean shutdown.
- Activating the app or launching a second instance restores and focuses the single window.

### Signed Auto-Updater (`internal/desktop/updates.go`)

- Configured in `mygo.config.ts` with `github: "duanyongcheng/gomail"`, an embedded Ed25519 public key, and `deltas: 3` (bsdiff incremental patches generated in `.github/workflows/release.yml` using `MYGO_UPDATER_PRIVATE_KEY`).
- `UpdateService` provides `Status`, `Check`, `Install`, and `Relaunch`:
  - When `mygo.Updater.Enabled()` is true (packaged signed builds), `Check` fetches the signed platform manifest (`update-<target>.json`), and `Install` downloads, verifies, and stages either a bsdiff delta or full archive while streaming `app:update-progress`.
  - When native self-update is unavailable (development builds, package-manager installs, or missing manifests), `Check` falls back cleanly to a read-only GitHub Releases API check (`https://api.github.com/repos/duanyongcheng/gomail/releases/latest`) and offers a validated link to download manually.
  - Disabled automatically when `GOMAIL_SELFTEST=1`.

---

## Local Development & Testing

### Common Commands

```sh
# Install frontend dependencies and regenerate Go -> TypeScript bindings
bun install
bun run generate

# Start GoMail in development mode (Vite dev server + MyGO WebView)
bun run dev

# Frontend checks
bun run typecheck
bun run test
bun run build:web

# Full quality gate (Go tests, vet, race detector, mygo vet/generate, TS/Vitest/Vite, 5 cross-builds)
bun run check

# Keep temporary check logs and cross-compiled binaries
GOMAIL_CHECK_KEEP_ARTIFACTS=1 bun run check

# Local ad-hoc signed macOS app bundle -> build/darwin-<arch>/GoMail.app
bun run build:local
```

### Running Targeted Go Tests

```sh
# Fast unit tests without CGO
CGO_ENABLED=0 go test -count=1 ./internal/...

# Race detector on internal packages
CGO_ENABLED=1 go test -race -count=1 ./internal/...
```

### Synthetic Desktop Selftest

Run the end-to-end WebView selftest against local mock Gmail and TLS IMAP/SMTP/POP3 fixtures (`internal/testgmail`, `internal/testmail`) with an in-memory keyring and disposable profile:

```sh
CGO_ENABLED=0 go build -o /tmp/gomail-selftest .
GOMAIL_SELFTEST=1 \
GOMAIL_PROFILE_DIR=/tmp/gomail-test-profile \
GOMAIL_SELFTEST_DIR=/tmp/gomail-test-results \
/tmp/gomail-selftest
```

The selftest exercises startup, sync, search, privacy rendering, `gomail-attachment://`, attachment export, offline workflows, pending remote writes, drafts/outbox, and theme switching, writes its JSON report and screenshots to `GOMAIL_SELFTEST_DIR`, and exits automatically.
