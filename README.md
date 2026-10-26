# Pebble Go

Pebble Go is the independent Go + MyGO implementation of Pebble. The original
Rust/Tauri project remains at `../Pebble` and is not modified by this project.

## Current scope

- MyGO WebView desktop shell.
- Existing Pebble React UI copied into this independent project.
- Gmail OAuth/API provider with PKCE, encrypted isolated tokens, initial sync,
  History sync, labels, read/star flags and SQLite cache/search.
- Demo mode uses a local Gmail API fixture and starts without network access.
- Live Gmail mode is explicit and requires a Desktop OAuth client JSON path.
- Gmail send/delete/archive are disabled in this first milestone.

## Development

Requirements: Go 1.27+, Bun, and the platform WebView dependencies.

```bash
GOTOOLCHAIN=go1.27.3 go version
bun install
bun run generate
bun run dev
```

Build the packaged app:

```bash
bun run build
```

## Real Gmail mode

The default mode is a local fixture. To use a real Gmail account in this
isolated project, explicitly set:

```bash
export PEBBLE_GMAIL_LIVE=1
export PEBBLE_GMAIL_OAUTH_CONFIG=/absolute/path/client_secret_desktop.json
# Optional, broad Gmail scope for read/star/label changes:
# export PEBBLE_GMAIL_ALLOW_MODIFY=1
bun run dev
```

Then click **Google 登录** and complete authorization in the system browser.
Do not put OAuth JSON, tokens or mailbox data into Git. The project uses its
own MyGO app identifier, data directory and token namespace; it does not read
the old Pebble profile or keyring.

## Architecture

```text
React + TypeScript + Vite
  -> generated MyGO bindings / platform adapter
  -> MyGO desktop shell
  -> Go Gmail service and application core
  -> SQLite cache / FTS5 / encrypted token vault
```

`internal/gmail` owns OAuth, token refresh, Gmail REST and History sync.
`internal/mailcore` currently contains shared MIME/search/storage helpers used by
the first vertical slice. The next step is to split stable domain contracts
from prototype compatibility code before adding more providers.

## Relationship to old Pebble

This project is intentionally independent. It does not migrate or write the
old Pebble profile yet. Keep the old Rust/Tauri app available until data
migration, HTML privacy rendering, provider parity and offline operation
contracts are complete.
