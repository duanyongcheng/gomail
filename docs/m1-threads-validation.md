# M1 conversations and replies validation

Validated on 2026-10-28, baseline `29d2765`. No live Gmail account, real recipient,
old Pebble profile or system keyring was used.

## Automated gate

Final `bun run check` passed:

| Check | Result |
|---|---|
| CGO-free Go tests | Passed |
| Go vet and internal race tests | Passed |
| MyGO vet and generated bindings | Passed |
| TypeScript | Passed |
| Vitest | 131 files, 757 tests passed |
| Frontend production build | Passed |
| darwin/amd64, linux/amd64+arm64, windows/amd64+arm64 CGO-free compilation | Passed |

New coverage includes:

- Identical native thread IDs in separate accounts, different threads with equal
  subjects, multi-folder deduplication, latest selected subject/snippet, participant
  addresses containing delimiter characters, chronological metadata-only detail.
- Header fallback with out-of-order parent/child, missing ancestors, cycles,
  soft-deleted parent, native authority and no subject-only grouping.
- Stable 61-thread paging, malformed handles/invalid paging, 500-message detail
  bound, cancellation, checkpoint rollback and V26→V27 existing-cache migration
  with the unchanged Rust v20 schema oracle and account cascades.
- Cc/Bcc and reply/reference parsing, malformed optional headers, long chain
  bounds, Unicode subject normalization, repeated Gmail sync persistence.
- Reply sends include the native thread ID and complete available References;
  changed subjects omit provider grouping, cross-account parent is rejected, and
  older encrypted outbox payloads without thread metadata still decode.
- Typed thread commands, infinite-query page overlap, load-more/error/retry UI,
  account selection for combined-inbox reply, paginated-cache keyboard navigation,
  expanded-only read marking, full-message quote retrieval and stale render errors.

During final review the keyboard cache consumer was found still expecting a flat
array after thread queries became paginated. It was adapted and covered by an
explicit next-message-across-pages test. A query-hook test initially asserted
before React Query had published its update; it now waits for observable state.

## Packaged desktop

- Final macOS arm64 package and strict codesign verification passed.
- All **15 packaged selftest checks** passed. The new check switches the actual
  inbox into Conversation view, opens a native thread containing two messages
  and verifies both bubble headers. The expanded message body and reply controls
  were inspected in `thread.png`.
- Existing queue ordering, attachment export, failed-download recovery, local
  draft restore/save and single mock send checks remain green.
- Normal empty profile stayed alive, created schema v27 with zero accounts and
  thread-index rows, then exited cleanly on SIGTERM (code 0).

Real Gmail's final server-side reply grouping and Windows/Linux runtime are not
claimed by these fixture/cross-compilation checks. Existing nonfatal Vite chunk
size warnings remain. Large header-only mailbox performance and per-conversation
pagination beyond 500 messages remain future work.

## Evidence

- Gate: `/tmp/pebble-go-threads-check-final.log`.
- Detailed logs/artifacts:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-q1jsEi/`.
- Package: `/tmp/pebble-go-threads-package-final.log`.
- Final desktop reports, screenshots and disposable profiles:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-threads-final-84zmhnk4/`.
- Bundle: `build/darwin-arm64/Pebble Go.app`.

Evidence is machine-local. See [implementation and limits](m1-threads.md).
