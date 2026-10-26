# M1 privacy renderer validation

Validated on 2026-10-26 after commit `503266b`. Real Gmail was not used.

## Automated gate

`bun run check` passed:

- CGO-free Go tests, Go vet and internal race tests.
- MyGO vet and generated bindings.
- TypeScript and Vitest: 125 files, 723 tests.
- Vite production build.
- CGO-free darwin/amd64, linux/amd64+arm64 and windows/amd64+arm64 builds.

The new tests cover backend XSS/CSS/URL sanitization, all privacy modes,
tracker/pixel reporting, full-document body extraction, plain-text fallback,
unknown and matching CID handling, attachment path containment and account/
message authorization, service privacy-mode forwarding, typed command
compatibility and frontend defense-in-depth handling of `pebble-attachment`.

## Packaged desktop

`bun run build -skip-dmg -skip-notarize` completed for the macOS arm64 app and
`codesign --verify --deep --strict` passed. The packaged selftest passed all
seven checks:

- React Inbox rendering.
- Persisted account retrieval.
- Typed provider/search.
- Opaque History cursor.
- CID attachment URL returned by backend privacy rendering.
- Download failure list rendering.
- Failed download retry through the real settings UI and generated bindings.

The selftest uses a local synthetic Gmail server and a MIME message containing a
CID inline PNG. It verifies the `pebble-attachment://` URL at the typed service
boundary; protocol authorization is covered by desktop handler tests. A separate
normal empty-profile packaged run stayed alive, created schema v23 with zero
accounts and exited cleanly on SIGTERM.

## Evidence

- Full gate: `/tmp/pebble-go-m1-privacy-check.log`.
- Detailed logs/build artifacts:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-vq0iqD/`.
- Package log: `/tmp/pebble-go-m1-privacy-package.log`.
- Packaged selftest profile/report: `/tmp/pebble-go-m1-privacy-bc5wlchq/`.
- Normal empty profile: `/tmp/pebble-go-m1-privacy-normal-lvs1cu2_`.
- App: `build/darwin-arm64/Pebble Go.app`.

Temporary evidence is machine-local. See [privacy contracts](../.trellis/spec/Pebble-Go/core/m1-privacy-contracts.md)
and [implementation notes](m1-privacy.md). Remaining M1 work includes durable
remote operations/outbox, labels, threading and sending.
