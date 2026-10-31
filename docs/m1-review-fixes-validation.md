# M1 code-review fixes validation

Validated on 2026-10-30 in `/Users/bary/code/tools/Pebble-Go`, preserving the
pre-existing uncommitted account-settings/cloud-draft/hardening implementation.
No commits or pushes were made. All mailbox traffic used synthetic loopback
Gmail, disposable profiles and the in-memory test keyring. No real mailbox,
recipient, system-keyring or external URL destination was used.

## Fixes covered

1. **Cloud-linked send retention:** the latest encrypted local draft/source/quote
   and attachment references survive Queue → frontend Delete/Cleanup → restart →
   cancel. Queued/failed/unknown reservations block restore/save/requeue; cancel
   releases the retained snapshot, while accepted/Check Sent receipt commits
   cleanup atomically. Statement/commit failures preserve both evidence and refs
   and never repeat submission. Legacy linked records whose local snapshot was
   already deleted reject destructive cancellation and keep their outbox bytes.
2. **Lossless editor-mode boundary:** imported HTML with CID images, tables or
   unsupported attributes/formatting stays in HTML mode after a rejected rich or
   Markdown conversion. Static en/zh alerts are visible; saving/sending remains
   enabled and keeps exact source/attachment refs and a separate quote. Real
   hook/UI tests also cover ordinary supported and combined bold/italic formats,
   no live transaction/undo changes on refusal, and no active remote-resource
   parsing during conversion preflight.
3. **Fresh conversation read:** expanded threads fetch local metadata independently
   of HTML. Cached read flags, browser offline state, cancelled initial reads and
   late navigation cannot consume the one-shot action incorrectly. Explicit
   mark-unread while still open remains respected.
4. **Shared flag lifecycle:** keyboard/palette and hook actions share QueryClient
   mutation ownership, targeted rollback and all affected view refreshes. Partial
   patches preserve authoritative timestamps; sync/queue events defer message
   refetches over provisional flags. Local metadata/flag IPC uses `networkMode:
   always`, independent of Gmail replay availability.

## Independent recheck

A separate check pass found and fixed offline mutation/query pausing, freshness
promotion by partial optimistic patches, initial-read cancellation during star
mutations, and false rejection of commuting bold/italic mark nesting. Targeted
combined recheck: **26 files / 253 tests passed**, plus backend store/app tests,
vet and typecheck. Main UI integration: **7 files / 106 tests passed**.

The first full gate found one outdated availability-test expectation: expanded
threads now perform an additional fresh local Get before the later reply Get.
The assertion was updated to require that exact path; product behavior was not
weakened. The full gate below was rerun after that correction.

## Full automated gate

`bun run check` passed:

- CGO-free Go tests and Go vet
- internal Go race tests
- MyGO vet and binding generation
- TypeScript
- Vitest: **157 files / 1088 tests**
- frontend production build
- darwin/amd64, linux/amd64+arm64, windows/amd64+arm64 compilation

`git diff --check` passed. Binding generation only added the documented protected
Delete cleanup comment; no DTO/signature or schema migration was required.

Gate log: `/tmp/pebble-review-fix-workers/final-check-2.log`.
Detailed logs/artifacts:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-HLfXIs`.

## Packaged macOS validation

Build command (OAuth values explicitly empty):

```sh
GOOGLE_CLIENT_ID= GOOGLE_CLIENT_SECRET= \
MICROSOFT_CLIENT_ID= MICROSOFT_CLIENT_SECRET= MYGO_LDFLAGS= \
bun run build -skip-dmg -skip-notarize -sign -
codesign --verify --deep --strict 'build/darwin-arm64/Pebble Go.app'
```

Build and strict verification passed. The bundle is **ad-hoc signed**, not a
Developer ID/notarized distribution release.

Packaged synthetic selftest passed **47/47** (40 prior checks + 7 review checks):

- actual UI cloud import/edit/autosave/Send, followed by post-queue cleanup;
- deterministic observed remote-revision conflict before submission, zero send
  requests, immutable queued content and retained encrypted local snapshot;
- real Outbox Cancel and confirmation, local-draft restore with latest source,
  quote and authenticated attachment bytes;
- visible rich/Markdown conversion refusal on an imported CID/table fixture,
  followed by exact HTML/source/reference preservation through Save and close;
- safe local discard cleanup leaving both remote fixture drafts untouched;
- prior lifecycle, single-instance, sync, account, privacy and sending checks.

The cancellation fixture seeds separated quote metadata through the public
compose service only while the editor is closed; current subject/body edits,
queue/cancel/restore/discard use actual UI and generated bindings. Attachment
verification reads only the explicit in-memory selftest key, not system secrets.

Observed standalone/conversation local read latency with stalled synthetic
Gmail: **39 ms / 32 ms**. Screenshots of restored compose and conversion refusal
were inspected. The selftest process exited normally with code 0.

Ordinary non-selftest empty profile stayed running for five seconds, then exited
code 0 after SIGTERM; schema **v31**, zero accounts. No native processes were left
running.

Runtime report, screenshots, logs and disposable profiles:
`/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-review-fixes-runtime-3f6_fcus`.
Package/native logs: `/tmp/pebble-review-fix-workers/final-package.log` and
`/tmp/pebble-review-fix-workers/final-native.log`.

## Boundaries / follow-up

- The offline local-workflow follow-up is now validated in
  [m1-offline-local-workflows-validation.md](m1-offline-local-workflows-validation.md):
  supported local reads and acknowledgement-first actions no longer pause on
  browser connectivity, while provider refresh and advanced search retain their
  explicit remote boundary.
- Live Google consent/refresh, real recipient delivery, system keyring and
  Windows/Linux runtime acceptance remain separate. Cross-compilation is not
  platform runtime evidence.
- Legacy linked sends with an already-deleted local snapshot are protected from
  further loss, not reconstructed; cancellation explicitly fails and retains
  their last outbox content.
- The conversion guard is conservative, not universal arbitrary-HTML support.
- The prior review's list-row star, batch/archive acknowledgement paths and
  supported local list/count/outbox queries are covered by the follow-up above.
  Contacts and local-label compatibility stubs remain unavailable and are not
  part of the supported offline surface.
- Temporary artifact paths are machine-local and may be removed by cleanup.
