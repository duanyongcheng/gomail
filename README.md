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
offline changes survive restart and retry with backoff. Same-message writes
replay in durable order; pending local changes survive concurrent sync, and unknown
outcomes are not auto-retried. Cached attachments can be downloaded or saved
through the native Save As dialog. Compose supports local encrypted drafts and
attachment staging. Gmail sending uses an encrypted durable outbox with primary
mailbox verification and unknown-outcome protection. Conversation view groups
cached mail by account-scoped Gmail threads and carries reply context through
sending. Gmail custom labels can be created, renamed and deleted from account
settings. Sender trust is persisted per account and enforced by the backend
privacy policy. Account-level access states and same-mailbox Google reconnection
preserve cached mail and queued work when authorization needs attention. Gmail sender name,
local account label, color and encrypted signature are editable offline; these values
save atomically without changing mailbox identity. Gmail proxy settings and mailbox
reassignment remain unavailable. Saved local drafts can be explicitly uploaded to
Gmail, browsed/imported, checked, updated and safely deleted with durable
unknown-outcome protection. Cloud-linked sends use Gmail's draft send operation so
the matching remote draft is consumed only after Gmail accepts the send; conflicts
and unknown outcomes remain visible. See [Gmail completion](docs/m1-gmail-completion.md).
IMAP/SMTP are tested as an isolated protocol prototype and join the app in M2.
Daily-use hardening adds WAL/FULL write durability, native close/restore behavior,
safe user-clicked web links, and shared unavailable-feature guards. See
[the hardening contract](docs/m1-daily-use-hardening.md).
Supported local mail views and durable acknowledgement-first actions also remain
usable when the browser reports offline; provider refresh, advanced search and
other remote actions keep their explicit network boundaries. See
[offline local workflow validation](docs/m1-offline-local-workflows-validation.md).

## Development

Requirements: Go 1.27.3, Bun, Node for Vite/Vitest, and platform WebView support.

```sh
bun install
bun run generate
bun run dev
```

### Local macOS build

```sh
bun run build:local
# Or run the script directly, from any working directory:
./scripts/build-local.sh
```

The script checks Go/Bun/Node/codesign, installs dependencies with the frozen
Bun lockfile, regenerates bindings, type-checks/builds the frontend, and packages
an ad-hoc signed app for the current Mac's architecture. It reuses the existing
`.env` OAuth build configuration without sourcing it as shell code. DMG creation
and notarization are skipped; the bundle signature is verified before success.
The native app is written to `build/darwin-<arch>/Pebble Go.app`, and the script
prints its path and an `open` command. It does not launch/install the app or
modify your profile. For Apple Silicon:

```sh
open "build/darwin-arm64/Pebble Go.app"
```

For a DMG or other platform targets, keep using `bun run build` with MyGO flags.
The local script replaces the target directory after a successful build, so a
previous DMG in that directory is removed rather than left as a stale package.

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

The privacy banner can remember images-only or full sender trust per account.
Images-only trust keeps trackers blocked; full trust requires confirmation.
Settings → Privacy manages exact-address trust and revocation. The backend
verifies persisted permission instead of trusting a frontend mode flag. See
[sender trust](docs/m1-trusted-senders.md).

The message attachment footer offers Download (system Downloads directory) and
Save As (native file chooser). Existing files are preserved using numbered
filenames, with progress and the actual saved path shown in the UI. Export and
CID reads share a symlink-contained cache opener; failed copies are cleaned up.
See [attachment export](docs/m1-attachment-export.md).

Flags, archive/restore, trash, moves, batch actions and mark-all-read commit
locally and enqueue a remote write in one transaction. A background worker
replays the queue with backoff; Settings → Remote Writes shows the queue, next
retry and failures, and can dismiss them. Permanent delete is unavailable with
`gmail.modify`. See [remote operations](docs/m1-remote-ops.md) and the
[reliability follow-up](docs/m1-remote-ops-reliability.md).

Compose auto-saves locally after three seconds. Use **Save and close**, then
open a blank compose → **Local drafts** to continue editing. Content and staged
attachment bytes are encrypted with the profile key; autosave does not contact
Gmail. The list offers explicit **Upload to Gmail**, **Update Gmail copy** and
read-only **Check Gmail copy**, plus Gmail draft browsing/import and explicit
conflict recovery/deletion. Deleting locally does not delete Gmail content.
Linked sending consumes the matching remote draft only after Gmail accepts it;
its latest local snapshot is retained until cancellation or acceptance. Unsafe
HTML-to-rich/Markdown conversions are refused without changing the source. See
[compose and drafts](docs/m1-compose-drafts.md) and
[the complete draft lifecycle](docs/m1-gmail-completion.md).

Send first saves a complete encrypted MIME snapshot to Outbox. The background
worker checks the Gmail primary mailbox, submits once, and distinguishes Gmail
acceptance from failed or unknown outcomes. Unknown sends are never automatically
resent; use **Check Sent** in Compose → Outbox or Settings → Remote Writes.
Queued acceptance is not delivery confirmation. See [Gmail outbox](docs/m1-gmail-outbox.md).

Conversation view supports paginated thread lists, expanded privacy-rendered
messages, attachments and replies. Native Gmail thread IDs are account-scoped;
header fallback never merges mail by subject alone. Replies preserve parent
References and, for unchanged subjects, the original Gmail thread ID. See
[conversations and threading](docs/m1-threads.md).

Settings → Accounts → **Gmail labels** manages server labels online, with system
label protection and explicit confirmation before deleting a custom label.
Deleting a label does not delete its messages. Uncertain writes are retained for
read-only refresh/reconciliation instead of automatic retries. See
[Gmail labels](docs/m1-gmail-labels.md).

Settings → Accounts → **Gmail connection** shows each account's local access
state and offers **Reconnect** / **Cancel sign-in**. Bad credentials in one account
do not prevent other accounts from restoring. Confirmed authorization failures
pause that account's sync and queued work without exhausting retry attempts;
temporary network failures retain normal backoff. Reconnection verifies the same
Gmail mailbox, preserves account settings/mail/drafts, and keeps unknown sends
unknown. See [account access and reconnection](docs/m1-account-reauth.md).

Settings → General → **Quit app when window is closed** controls native close
behavior. Otherwise the existing window hides and background workers continue;
application activation or another launch restores that same window and compose
state. Explicit Quit always shuts down. Notification/tray/autostart and hidden
startup features remain unavailable. HTTP(S) message/About links open through a
validated Go service; failures show a generic error without exposing private URLs.

Unsupported features are disabled or hidden across settings, navigation, command
palette, keyboard shortcuts and mail/compose actions. These include advanced
search filters, non-Gmail setup, proxies, local labels, contacts, Kanban, snooze,
rules, templates, AI, translation and backup. Basic search, local appearance and
supported Gmail workflows remain available; empty-trash/permanent delete is still
unavailable with Gmail modify access.

Settings → Accounts → **Edit account** saves the Gmail sender name, account label,
color and encrypted signature together. Signature reads require the profile key
and must complete before settings can be saved. Gmail proxy changes and mailbox
address reassignment are unavailable; use same-mailbox Reconnect for authorization
repair. Queued messages retain their original sender and content snapshot. See
[account editing and signatures](docs/m1-account-settings.md).

## Gmail OAuth configuration

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
use local fixtures only. Sending is supported through Outbox; permanent delete
remains unavailable with `gmail.modify`.

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
read-only connections, WAL, foreign keys and a 15-second busy timeout. Every
writer connection uses `synchronous=FULL`, including pool replacement, to strengthen
committed outbox/cloud/label intent durability before external side effects. This
is not an exactly-once delivery guarantee; unknown outcomes still require checks. Legacy
migrations v1–v20 match the Rust schema; Go adds the FTS5 search cache in v21
and the account-scoped backfill reconciliation table in v22. Version 23 adds
persistent Gmail message download failures and retry timing. Version 24 adds a
Go-only operation-sequence sidecar for FIFO replay and sync reconciliation.
Version 25 adds separate encrypted local drafts, staged attachment objects and
reference tracking; these are not part of remotely synchronized mail or FTS.
Version 26 adds the encrypted send outbox, independent of idempotent mail mutations.
Version 27 adds an account-scoped conversation index without modifying legacy tables.
Version 28 records unresolved Gmail label-management writes for safe reconciliation.
Version 29 adds encrypted account-scoped Gmail signatures. Version 30 records
Gmail cloud-copy identities and durable write intents. Version 31 adds cloud
copy deletion/send lifecycle states and outbox association. These additions do
not change the Rust v20 schema. An unversioned PoC database is rejected instead of being silently
rewritten.
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
inbox, conversation view, Gmail label CRUD, sender trust/revocation, local account
settings and encrypted signatures, attachment export, local draft restore/save,
Gmail cloud-copy upload/check/update, failed-download retry, same-mailbox reconnection with unknown-send and draft
preservation, and a single send to the local Gmail fixture through the UI. Hardening
checks exercise real message-link routing to an in-memory opener, unavailable
settings, close/restore with edited compose state, a real second instance, and
native quit-on-close. The selftest uses a separate application name/instance lock. It
writes a JSON report and screenshots, then exits. It does not
contact Google or the system keyring; draft checks use an in-memory test keyring:

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
