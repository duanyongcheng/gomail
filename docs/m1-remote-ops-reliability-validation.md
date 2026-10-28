# M1 remote operations reliability validation

Validated on 2026-10-28, baseline `848ac91`. No real Gmail, production profile,
old Pebble checkout, or keyring was used.

## Reproductions before the fix

The following regression tests failed against the previous implementation:

- `TestQueuedWritesRetryWithBackoffAndNeverAutoRetryUnknownOutcome`: a second
  pass called the provider again after the unknown result.
- `TestArchiveTogglesUsingInboxMembership`: local Inbox membership remained true
  immediately after archive (the old test manually removed it).
- `TestEveryBatchMutationWakesPendingWorker`: batch flags, account mark-all-read
  and folder mark-all-read all lacked a worker wakeup.

## Final automated gate

`bun run check` passed:

| Check | Result |
|---|---|
| CGO-free Go tests | Passed |
| Go vet and internal race tests | Passed |
| MyGO vet and binding generation | Passed |
| TypeScript | Passed |
| Vitest | 127 files, 733 tests passed |
| Frontend production build | Passed |
| darwin/amd64, linux/amd64+arm64, windows/amd64+arm64 CGO-free build | Passed |

Added coverage includes:

- Queue/local/sequence rollback, cross-account target rollback, duplicate/missing
  IDs, trash/restore/move memberships and closed-database persistence.
- Same-message FIFO during backoff/unknown/in-progress, timestamp ties,
  concurrent toggles and claims, and dismiss releasing the successor.
- Active and successful intent overlays, fresh-read reconciliation of external
  changes, checkpoint rollback of receipt release, and confirmed deletion.
- V23→V24 sequence seeding/cascade and unchanged Rust v20 schema oracle.
- Atomic startup recovery, legacy queue-only local-effect repair, recorded
  success without remote replay, and exclusion of unknown markers even pending.
- Injected success/failure/unknown settlement errors: only SQLite is retried and
  provider calls stay at one. Recovery failures prevent remote calls.
- A 45-message backlog drains without waiting for another 30-second poll.
- Gmail mock sync blocked during a concurrent archive, then replay/fresh sync
  convergence, later external changes, and Trash→Inbox remote labels.
- Pending events refresh account/folder unread counts.

Concurrency/FIFO/settlement/provider tests were additionally repeated five times.

## Packaged desktop

- macOS arm64 package and `codesign --verify --deep --strict` passed.
- The final packaged selftest passed all **11 checks**. It now asserts immediate
  local archive, then restore→archive ordering, drains the queue and performs
  an authoritative full scan to verify the archived message stays out of Inbox.
  Attachment UI export and failed-download settings recovery still pass.
- Inspected the inbox screenshot: the archived `Queued remote write` message is
  absent, rather than remaining in Inbox as in the older evidence.
- A normal empty profile stayed alive, had schema v24, zero accounts and zero
  pending operations, and exited cleanly with SIGTERM (code 0).

Windows/Linux are compile-only, not runtime validation. Vite still reports the
existing nonfatal large-chunk warning. Native Save As picker interaction and real
Google account behavior are outside this local queue-validation slice.

## Evidence

- Final gate: `/tmp/pebble-go-remote-ops-reliability-check-final.log`.
- Detailed gate artifacts:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-702QTr/`.
- Package: `/tmp/pebble-go-remote-ops-reliability-package-final.log`.
- Final desktop report, screenshots and temporary profiles:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-remote-ops-reliability-final-2kooj1rc/`.
- Bundle: `build/darwin-arm64/Pebble Go.app`.

Evidence is machine-local. See [implementation and retrospective](m1-remote-ops-reliability.md).
