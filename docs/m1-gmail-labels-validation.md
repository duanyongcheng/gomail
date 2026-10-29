# M1 Gmail server labels validation

Validated on 2026-10-29, baseline `30ccc7d`. All remote operations used a synthetic
loopback Gmail fixture. No live mailbox, real Gmail mutation, old Pebble profile
or system keyring was used.

## Automated gate

Final `bun run check` passed:

| Check | Result |
|---|---|
| CGO-free Go tests | Passed |
| Go vet and internal race tests | Passed |
| MyGO vet and generated bindings | Passed |
| TypeScript | Passed |
| Vitest | 132 files, 765 tests passed |
| Frontend production build | Passed |
| darwin/amd64, linux/amd64+arm64, windows/amd64+arm64 CGO-free compilation | Passed |

Coverage added:

- POST/PATCH/DELETE payloads, DELETE 204/404, malformed success, wrong response
  identity, 403/429/500/redirect classification, and no automatic write retry.
- Empty, incomplete and duplicate label lists cannot become deletion snapshots;
  UTF-8 names, byte length, reserved names and control characters are validated.
- Create/rename retains stable folder identity; deletion removes memberships but
  preserves mail and attachments. Normal sync does not recreate a deleted label.
- System labels, another account's folder, duplicate names and mailbox identity
  mismatch are rejected without writes.
- Lost create/rename/delete responses leave persistent intents. After database
  reopen, read-only refresh reconciles them without a second write.
- Definite rejection clears the intent; injected post-success database failure
  keeps it until refresh. Failed reconciliation rolls back folders and marker.
- Delete is blocked by queued mail changes; a delete intent blocks new moves;
  external deletion retires pending targets and replay rejects a stale claimed
  payload instead of calling Gmail with the removed label.
- Account isolation, stale dismiss IDs, unmatched intents, V27→V28 upgrade and
  unchanged independent Rust v20 schema oracle.
- Service list is local-only. UI cached read/explicit refresh, create/rename,
  confirmation/cancel, system protection, uncertain-state blocking, busy guard,
  account switching, stale responses and read/write errors.
- Folder-change events invalidate mail, counts and queue views; removing the
  selected folder resets the sidebar to an available Inbox.

## Packaged desktop

- Final macOS arm64 package and strict codesign verification passed.
- All **16 packaged selftest checks** passed. The new check uses the real settings
  UI to create, rename and delete a label; the fixture records exactly three label
  writes, both local/remote lists omit the deleted label and the intent is cleared.
- Existing conversation, draft, send, queue, attachment and recovery checks remain
  green. Inspected `gmail-labels.png`: custom labels appear first, system labels
  show no mutation buttons, and the Create button stays on one line.
- Empty-profile startup stayed alive, created v28 with zero accounts and label
  intents, and exited cleanly on SIGTERM (status 0).

Windows/Linux are compilation-only, not runtime validation. Real Google label
behavior, nested-label UX and authorization remain manual verification. Existing
nonfatal Vite chunk-size warnings remain. No offline label replay is claimed.

## Evidence

- Gate: `/tmp/pebble-go-gmail-labels-check-final.log`.
- Detailed gate artifacts:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-uhyEtP/`.
- Package: `/tmp/pebble-go-gmail-labels-package-final.log`.
- Final desktop reports, screenshots and disposable profiles:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-gmail-labels-final-iwe25ub1/`.
- Bundle: `build/darwin-arm64/Pebble Go.app`.

Evidence is machine-local. See [implementation and boundaries](m1-gmail-labels.md).
