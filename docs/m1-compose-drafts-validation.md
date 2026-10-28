# M1 local compose/drafts validation

Validated on 2026-10-28 against baseline `a521b8b`. No real Gmail or old Pebble
profile was used; desktop draft checks use an in-memory keyring only.

## Automated gate

Final `bun run check` passed:

| Check | Result |
|---|---|
| CGO-free Go tests | Passed |
| Go vet and internal race tests | Passed |
| MyGO vet and binding generation | Passed |
| TypeScript | Passed |
| Vitest | 128 files, 744 tests passed |
| Frontend production build | Passed |
| darwin/amd64, linux/amd64+arm64, windows/amd64+arm64 CGO-free compilation | Passed |

New regression coverage includes:

- MIME From/Date/Message-ID and reply/reference headers, Unicode body/attachment
  roundtrip, multipart/alternative, explicit Bcc policy, header injection rejection
  and bounded encoded output.
- Encrypted draft/attachment storage, account isolation, database close/reopen,
  create retries reusing one ID, tombstones blocking late saves, revision CAS and
  injected reference-transaction rollback.
- Released attachments surviving while referenced by another draft/account,
  then deletion after the final reference; account cascade cleanup.
- Missing/wrong DEK refuses overwrite, tampered attachment refuses restore/save,
  arbitrary filesystem paths and invalid byte values rejected.
- V24→V25 upgrade preserves accounts and the independent Rust v20 schema oracle.
- Typed command routing, local draft list/restore/delete/read failure, dirty-editor
  replacement protection, stable retry key, restore/update using the existing ID,
  saving/failed/saved states, stale-save status, quote/source separation,
  Save and close, failed-discard preservation and pre-read attachment limits.

One regression was found while adding save-status state: status rerenders could
create new array identities and restart an exhausted autosave retry budget. The
hook now keys autosave scheduling on semantic snapshot content, and the bounded
retry test remains enforced. Pending recipient input is included in drafts.

## Packaged desktop

- Final macOS arm64 package and strict codesign verification passed.
- Packaged selftest passed all **13 checks**. New checks restore an encrypted
  draft and retained attachment through the actual Local drafts UI, edit its
  subject, click Save and close, and verify there is still exactly one updated
  draft with its attachment.
- Screenshot `local-draft.png` was inspected: the composer displays local-only
  notice, restored subject/body/attachment, and Save and close; Send is disabled.
- Normal empty-profile startup stayed alive, created schema v25 with zero
  accounts/drafts and exited on SIGTERM with status 0.

MIME tests validate building only, not real delivery. Gmail cloud drafts, sending,
real keyring integration for this new flow, Windows/Linux runtime, and manual
native picker interaction remain outside this automated validation. The existing
nonfatal Vite chunk-size warning remains.

## Evidence

- Gate: `/tmp/pebble-go-compose-drafts-check-final.log`.
- Detailed logs/artifacts:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-CqH3Mg/`.
- Package: `/tmp/pebble-go-compose-drafts-package-final.log`.
- Final desktop report, screenshots and disposable profiles:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-compose-drafts-final-h9uu0obv/`.
- Bundle: `build/darwin-arm64/Pebble Go.app`.

Evidence is machine-local. See [implementation and limits](m1-compose-drafts.md).
