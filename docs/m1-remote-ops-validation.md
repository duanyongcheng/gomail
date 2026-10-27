# M1 remote operations validation

Validated on 2026-10-26 after commit `9e77ad0`. Real Gmail was not used.

## Automated gate

`bun run check` passed:

| Check | Result |
|---|---|
| CGO-free Go tests | Passed |
| Go vet and internal race tests | Passed |
| MyGO vet and binding generation | Passed |
| TypeScript | Passed |
| Vitest | 126 files, 727 tests passed |
| Vite production build | Passed |
| CGO-free darwin/amd64, linux/amd64+arm64, windows/amd64+arm64 | Passed |

New tests cover atomic queue/local rollback, transactional claim, backoff and
retry cap, account isolation, interruption recovery, unknown-outcome
non-retry, dismiss, legacy pending-op upgrade, provider replay payloads,
cancellation, local-first flags, wake-on-enqueue, batch and all-read scoping,
archive toggle, folder ownership, permanent-delete rejection, typed command
routing and the Remote Writes tab states.

Two real defects were found and fixed during this slice:

1. A provider returning "outcome unknown" was still completed as success. It now
   becomes a non-retryable `outcome_unknown` row.
2. Queued writes waited up to 30 seconds for the polling tick. Enqueue now wakes
   the worker immediately, with the timer as a fallback.

## Packaged desktop

`bun run build -skip-dmg -skip-notarize` and
`codesign --verify --deep --strict` passed for the macOS arm64 bundle. The
packaged selftest passed all eight checks, including `queued remote write
replayed`: the app queues an archive, the worker drains it, and the pending
summary returns to zero. A normal empty-profile run stayed alive, created schema
v23 with zero accounts and zero pending ops, and exited cleanly on SIGTERM.

An earlier selftest run failed because the queued archive removed the only
message the UI checks inspected; the fixture now archives a separate message.
That was a test-sequencing issue, not a product regression.

## Evidence

- Gate: `/tmp/pebble-go-m1-remote-ops-check-final.log`.
- Detailed logs/artifacts:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-klTNBf/`.
- Final package: `/tmp/pebble-go-m1-remote-ops-package3.log`.
- Selftest and normal profile: `/tmp/pebble-go-m1-remote-ops3-fj1_766p/`.
- App: `build/darwin-arm64/Pebble Go.app`.

Temporary evidence is machine-local. See
[remote-ops contracts](../.trellis/spec/Pebble-Go/core/m1-remote-ops-contracts.md)
and [implementation notes](m1-remote-ops.md). Sending, drafts, labels CRUD,
threading and attachment export remain M1 work.
