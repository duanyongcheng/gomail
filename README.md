# Pebble Go

Pebble Go is the independent Go + MyGO WebView implementation of Pebble, using
the existing React UI. The Rust/Tauri reference at `../Pebble` is kept separate.

M0 establishes versioned storage, encrypted account restoration, typed services,
and explicit frontend command coverage. The first M1 slice adds resumable Gmail
mailbox backfill, paginated History sync and preference-controlled scheduling.
The next slices support larger messages and persistent recovery of failed
Gmail downloads, with a retry list in account settings, plus backend privacy
HTML/CID rendering. Gmail supports multiple accounts, local search, unread counts
and read/star changes. Mail mutations now use a durable remote-write queue, so
offline changes survive restart and retry with backoff. Cached attachments can
be downloaded or saved through the native Save As dialog. Sending/drafts, labels
CRUD and threading remain M1 work. IMAP/SMTP are tested
as an isolated protocol prototype and join the app in M2.

## Development

Requirements: Go 1.27.3, Bun, Node for Vite/Vitest, and platform WebView support.

```sh
bun install
bun run generate
bun run dev
```

An empty profile opens a normal app and stays running. Token restoration does
not contact the network. Once the UI loads the profile's realtime preference,
configured Gmail accounts start syncing unless the preference is `manual`.
Use account settings to log in. Manual sync can always be triggered explicitly.
Message bodies are escaped plain text only when no HTML part exists; HTML is
sanitized by the backend privacy renderer before the WebView.

Backfill processes the whole mailbox in pages of at most 100 messages and
persists recovery checkpoints after each page. Each pass handles at most ten
pages; unfinished work continues after one second, including an explicitly
requested manual backfill. Stop cancels the current request. Realtime polling
uses 10 seconds while recently active, 30 seconds while idle and 120 seconds in
the background, with failure backoff up to five minutes. Balanced and battery
preferences use 30- and 60-second foreground intervals. Manual mode stops
scheduled polling and ignores focus/reconnect triggers. See
[the sync contracts and limitations](docs/m1-sync.md).

Gmail accepts raw messages and total decoded MIME content up to 50 MiB. A
malformed or oversized message is saved for retry while other mail continues
syncing; existing cached content stays available. Open Settings → Accounts →
Mail downloads to inspect failures and explicitly retry one account or all
accounts. Automatic modes retry eligible failures with backoff; manual mode
only processes retries as part of an explicit sync or retry action. See
[download recovery](docs/m1-recovery.md).

Message HTML is sanitized in the Go core before it reaches the WebView. Strict,
LoadOnce, Off and TrustedSender modes control external images and stylesheets;
trackers are reported in the privacy banner. CID inline images use the narrow
`pebble-attachment://` protocol and cannot address arbitrary profile files. See
[privacy rendering](docs/m1-privacy.md).

The message attachment footer offers Download (system Downloads directory) and
Save As (native file chooser). Existing files are preserved using numbered
filenames, with progress and the actual saved path shown in the UI. Export and
CID reads share a symlink-contained cache opener; failed copies are cleaned up.
See [attachment export](docs/m1-attachment-export.md).

Flags, archive/restore, trash, moves, batch actions and mark-all-read commit
locally and enqueue a remote write in one transaction. A background worker
replays the queue with backoff; Settings → Remote Writes shows the queue, next
retry and failures, and can dismiss them. Permanent delete is unavailable with
`gmail.modify`. See [remote operations](docs/m1-remote-ops.md).

## Gmail OAuth configuration

Copy `.env.example` to a local `.env` and set your Google Desktop client values:

```dotenv
GOOGLE_CLIENT_ID=your-desktop-client-id.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=your-desktop-client-secret
```

Bun loads `.env` during development. `bun run build` injects these local values
using `MYGO_LDFLAGS -X`. Process environment values override packaged defaults;
an explicitly empty process value disables a packaged value. Microsoft client
variables are reserved for M2. Without a Google client ID, the login button says
“未配置” and no authorization request is made.

Gmail requests `gmail.modify` plus `openid email profile`. Google authorization
opens in the system browser with PKCE and a one-shot loopback callback. User
consent and real Google account verification are manual steps; automated checks
use local fixtures only. No send or permanent-delete action is implemented.

OAuth tokens are encrypted in `accounts.auth_data` with the Pebble v1
AES-256-GCM envelope, bound to account identity. The DEK uses the independent
`dev.bary.pebble.go` keyring service. Refresh uses compare-and-swap persistence.
A locked keyring or missing DEK preserves the ciphertext and requires attention;
there is no plaintext fallback. The old Pebble keyring is never read.

Do not put OAuth values, tokens, profiles, or mailbox data in Git. `.env` files
are ignored; `.env.example` contains placeholders only.

## Profiles and storage

The application identifier is `dev.bary.pebble.go` and its name is `Pebble Go`.
Profile directory precedence matches the old app:

1. `PEBBLE_PROFILE_DIR` (relative values resolve beside the executable).
2. The first line of `pebble-profile.txt` beside the executable.
3. A `pebble-profile` directory beside the executable.
4. MyGO's application user-data directory.

A persistent `profile-id` isolates frontend preferences even if a profile is
moved. The database is `pebble.sqlite`; attachment content is stored under
`attachments/` and referenced by relative `attachments.local_path`. Logs rotate
under `logs/` and contain operation statuses, not credential or mail payloads.

`internal/store` uses `database/sql` + `modernc.org/sqlite`, one writer and four
read-only connections, WAL, foreign keys and a 15-second busy timeout. Legacy
migrations v1–v20 match the Rust schema; Go adds the FTS5 search cache in v21
and the account-scoped backfill reconciliation table in v22. Version 23 adds
persistent Gmail message download failures and retry timing.
An unversioned PoC database is rejected instead of being silently rewritten.
Old live-profile import remains M6 work: use a separate Pebble-Go profile.

## Checks and local desktop selftest

```sh
bun run test
bun run check
bun run build -skip-dmg -skip-notarize
```

`check` runs Go tests/vet/race, MyGO vet, binding generation, TypeScript,
Vitest, frontend build and CGO-free compilation for darwin/amd64,
linux/amd64+arm64 and windows/amd64+arm64. It prints the temporary directory
containing detailed logs and compile artifacts. Cross-compilation does not
claim Windows/Linux runtime validation.

All 130 upstream test files are present. Seven upstream-only suites are
excluded with individual reasons in `tests/skipped.json`: six Rust/Tauri config
suites and the separate public website suite. The remaining upstream tests run
alongside Pebble-Go adapter tests.

The explicit desktop selftest uses a synthetic local Gmail server, verifies the
inbox, attachment export and a failed-download retry through the settings UI,
writes a JSON report and screenshots, then exits. It does not contact Google or use the keyring:

```sh
CGO_ENABLED=0 go build -o /tmp/pebble-go-selftest .
PEBBLE_SELFTEST=1 \
PEBBLE_PROFILE_DIR=/tmp/pebble-go-test-profile \
PEBBLE_SELFTEST_DIR=/tmp/pebble-go-test-results \
/tmp/pebble-go-selftest
```

Use a dedicated temporary profile for selftest. Without `PEBBLE_SELFTEST=1`, the
fixture and automatic exit are disabled.

## Architecture and compatibility

```text
React / src/lib/api.ts
  -> explicit typed command table / src/lib/platform/
  -> generated MyGO bindings
  -> application services / internal/app
  -> provider interfaces / internal/domain
  -> Gmail provider / internal/provider/gmail
  -> repositories / internal/store, crypto, oauth
```

There is no generic backend dispatcher. Each of the 145 invoke commands in the
copied frontend is listed in [docs/parity.md](docs/parity.md). Implemented
commands call generated services, optional startup stubs return explicit safe
values, action stubs throw `NotImplemented` with the command name, and unknown
commands fail.

To regenerate the independent Rust v20 schema oracle without changing the
reference checkout:

```sh
python3 scripts/generate_legacy_golden.py ../Pebble
```

The generator creates a temporary Cargo project with a path dependency on the
reference `pebble-store`, opens a synthetic database and records the normalized
schema in `internal/store/testdata/legacy_v20.json`. It does not open profiles.
