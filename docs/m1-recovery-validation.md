# M1 download recovery validation

Validated on 2026-10-26, starting from `1d29119`. Automated Gmail operations used
synthetic accounts and local servers only.

## Quality gate

`bun run check` passed on the final implementation:

| Check | Result |
|---|---|
| CGO-free Go tests | Passed |
| Go vet and internal race tests | Passed |
| MyGO vet and binding generation | Passed |
| TypeScript | Passed |
| Vitest | 125 files, 722 tests passed |
| Frontend production build | Passed |
| CGO-free darwin/amd64 build | Passed |
| CGO-free linux/amd64 + arm64 builds | Passed |
| CGO-free windows/amd64 + arm64 builds | Passed |

Sixteen new Go tests and seven frontend cases cover message recovery. A real
9 MiB MIME attachment crosses the former JSON/MIME limits and persists with
identical bytes. Malformed messages no longer block later mail, survive a
DB reopen and recover without duplicate new-mail announcements. Transient
HTTP/auth/quota/server failures and cancellation retain the page checkpoint.
The retry budget, cooldown, committed large-message prefix, preserved old
cache/attachments, queue rollback, cascade cleanup and v22→v23 migration are
covered. Service concurrency tests reproduce a retry concurrent with a
finishing shared sync pass. UI cases cover list states, filters, paging,
retry, progress refresh and localized download counts.

## Packaged desktop

`bun run build -skip-dmg -skip-notarize` and
`codesign --verify --deep --strict` passed for the final macOS arm64 bundle.
The explicit packaged selftest passed all six checks:

- React Inbox rendering.
- Persisted account retrieval.
- Typed provider and search.
- Opaque History cursor preservation.
- Failed-download list rendering in account settings.
- Clicking retry through the real UI/bindings and clearing the repaired failure.

The final failure-state screenshot was inspected, including the translated
pending count in the account card/status bar. Retry restored a second message
and its unread count. A separate empty-profile run stayed alive, created a v23
database with zero accounts and exited with code zero on SIGTERM.

## Evidence

- Full gate: `/tmp/pebble-go-m1-recovery-check-final.log`.
- Detailed check logs and cross-compiled binaries:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-nnz32e/`.
- Final package build: `/tmp/pebble-go-m1-recovery-package-final.log`.
- Desktop report, screenshots and disposable profiles:
  `/tmp/pebble-go-m1-recovery-final-wn3bs98a/`.
- App bundle: `build/darwin-arm64/Pebble Go.app`.

Temporary evidence is machine-local. Reproducible tests and
[recovery contracts](m1-recovery.md) are in the repository. Windows/Linux
results are compile checks; real Gmail long-running acceptance remains manual.
Messages above the 50 MiB bounds remain in the visible retry list. Privacy
HTML/CID, durable remote operations/outbox and the other M1 features remain
separate work.
