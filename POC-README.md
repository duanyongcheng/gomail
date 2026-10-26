# Historical Gmail OAuth/API slice reproduction

This document records the original isolated PoC. Current Pebble-Go startup,
OAuth configuration, profiles and checks are documented in `README.md`.
The old environment switches below are not supported by the current app.

This is an isolated Go + MyGO WebView prototype. It does not modify the Pebble repo, installed app, existing profile, keyring or mailbox.

## Local deterministic gate

```bash
cd /Users/bary/.trellis/tasks/10-26-pebble-go-gmail-oauth/poc
GOTOOLCHAIN=go1.27.3 CGO_ENABLED=0 go test -count=1 -v ./...
GOTOOLCHAIN=go1.27.3 CGO_ENABLED=1 go test -race -count=1 ./internal/...
GOTOOLCHAIN=go1.27.3 CGO_ENABLED=0 go vet ./...
node node_modules/typescript/bin/tsc --noEmit
node node_modules/vite/bin/vite.js build
GOTOOLCHAIN=go1.27.3 CGO_ENABLED=0 go build -o artifacts/gmail-poc .
MYGO_GENERATE=src/poc/mygo.ts artifacts/gmail-poc
artifacts/gmail-poc
```

The local run starts a mock Gmail OAuth/API server, creates synthetic RAW MIME mail, invokes OAuth/API sync through generated typed bindings, renders the original React inbox/detail/search, then exits. It never contacts Google. Final local report: `artifacts/gmail-runtime.json`, 14/14. The packaged macOS `.app` also reports 14/14 in `artifacts/gmail-packaged-runtime.json` and passes deep strict ad-hoc code-sign verification.

## Real Google mode (explicit user action only)

No Google client or token was used for the local gate. To attempt a real login yourself, do not put secrets in this repo:

```bash
export PEBBLE_GMAIL_LIVE=1
export PEBBLE_GMAIL_OAUTH_CONFIG=/absolute/path/to/client_secret_desktop.json
# Optional, broader Google scope; default remains gmail.readonly:
# export PEBBLE_GMAIL_ALLOW_MODIFY=1
artifacts/gmail-poc
```

Then click **Google 登录** in the app's top Gmail bar, complete consent in the system browser and click **同步最近邮件**. Without these variables the live mode cannot start. `gmail.modify` is intentionally opt-in; app send/delete/archive remain disabled.

## Explicit nonclaims

- Local mock is not Google and does not prove OAuth app verification, testing-user consent, quotas or live provider behavior.
- Initial cache is bounded to 100 recent messages; full mailbox/backfill/watch push is not implemented.
- Live body rendering intentionally escapes to plain text until the production sanitizer/CID service is ported.
- Windows/Linux compile-only; no runtime. Real Google login was not performed in the session because no client JSON/consent was supplied.

Artifacts include test logs, screenshots, frontend build, packaged run, cross-compile logs and copied-source hash check. `artifacts/original-source-hashes.json` confirms the original repo copy remained unchanged.
