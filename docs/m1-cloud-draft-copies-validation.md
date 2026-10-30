# Gmail cloud-copy slice validation

Validated locally on 2026-10-29. All mailbox/OAuth operations used synthetic
loopback fixtures. No real Google mailbox, consent flow or recipient was used.

## Final automated gate

`bun run check` passed:

- CGO-free Go tests and Go vet
- Internal Go race tests
- MyGO vet and generated bindings
- TypeScript
- Vitest: **136 files / 804 tests**
- Frontend production build
- darwin/amd64, linux/amd64+arm64, windows/amd64+arm64 cross-compilation

`git diff --check` passed. The nonfatal Vite large-chunk advisory remains.

Final gate log: `/tmp/pebble-cloud-drafts-final-gate.log`.
Detailed logs/binaries:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-zYgv0a`.

## Final packaged runtime

`bun run build -skip-dmg -skip-notarize` and
`codesign --verify --deep --strict` passed for macOS arm64.

The packaged desktop selftest passed **26/26**, including actual cloud-copy
upload → check → update UI actions against the local Gmail API. It verified two
writes produced one cloud draft, plus all previous account-settings, local draft,
outbox, reconnection, privacy, label and download checks. The final screenshot
was inspected after improving action spacing, button borders and wrapping.

Packaged report, screenshot, disposable profile and runtime log:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-cloud-drafts-final-runtime-3qq1dctb`.
Package log: `/tmp/pebble-cloud-drafts-final-package.log`.
Bundle: `build/darwin-arm64/Pebble Go.app`.

Normal empty-profile startup remained running until SIGTERM, with schema v30 and
zero accounts. Evidence:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-empty-v30-final-cbhxum0d`.

## Scope and next step

Account-settings revalidation is complete. This cloud-copy slice is tested but
**the full Gmail cloud-draft lifecycle is not complete**. Next: remote draft
listing/import, safe remote deletion, draft-aware outbox/send cleanup, Markdown
cloud rendering and explicit conflict recovery. Then enter production IMAP/SMTP.
See [implementation and concurrency limits](m1-cloud-draft-copies.md).

Live Gmail, real keyring and Windows/Linux runtime still require acceptance.
Temporary evidence paths are machine-local. All changes remain uncommitted.
