# M1 Gmail completion validation

Validated on 2026-10-30 in `/Users/bary/code/tools/Pebble-Go`. Automated mailbox
traffic used the synthetic loopback Gmail service and disposable profile; no real
Google consent, mailbox, recipient, system keyring or external destination was
used.

## Subsequent review validation

The review fixes were revalidated later on 2026-10-30: **157 frontend files /
1088 tests**, full Go/build gate, **47/47** packaged synthetic checks and normal
empty-profile startup passed. See [review-fixes validation](m1-review-fixes-validation.md)
for current evidence, retention/converted-source behavior and acceptance limits.
The original completion evidence below is retained as historical validation.

## Full automated gate

`bun run check` passed all configured checks:

- CGO-free Go tests and Go vet
- Go race tests
- MyGO vet and generated bindings
- TypeScript type check
- Vitest: **151 files / 958 tests**
- Frontend production build
- darwin/amd64, linux/amd64+arm64 and windows/amd64+arm64 cross-compilation

The gate also passed the read-state regression suites: metadata is loaded before
rendered HTML, caches update across inbox/starred/thread/detail views, and a
stalled remote write does not delay the local acknowledgement.

## Packaged desktop selftest

`bun run build -skip-dmg -skip-notarize` produced and signed
`build/darwin-arm64/Pebble Go.app`; strict codesign verification passed.

The disposable selftest completed **40/40** checks. It exercises actual generated
bindings and UI actions for:

- opening an unread standalone message and conversation while the synthetic Gmail
  server is held, with local read transitions measured at **45 ms** and **30 ms**;
- Gmail draft pagination, import, local edit/update, conflict-safe recovery,
  explicit remote deletion and local-copy retention;
- Bcc, attachment bytes, reply References and thread identity through import,
  update and linked send;
- Gmail draft consumption through `drafts.send`, with no fallback resend/delete;
- existing sync, labels, attachment export, outbox, account settings, reconnect,
  privacy, lifecycle and unavailable-feature checks.

Runtime report: `/tmp/pebble-gmail-completion-results-final/runtime.json`.
Read latency report: `/tmp/pebble-gmail-completion-results-final/read-latency.json`.
Package log: `/tmp/pebble-gmail-completion-package-final.log`.

## Remaining acceptance boundary

Live Gmail OAuth/refresh, recipient delivery, system-keyring access and Windows/
Linux runtime behavior still require manual acceptance. Cross-compilation does
not prove those platform runtimes. Production IMAP/SMTP remains M2 work.
