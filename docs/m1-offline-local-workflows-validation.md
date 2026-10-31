# M1 offline local workflows validation

Validated on October 31, 2026 in `/Users/bary/code/tools/Pebble-Go`. This
follow-up completes the remaining local-first query and action paths from the
October 30 review. It does not make Gmail synchronization or remote label
refresh run while disconnected.

## Local boundary

The following reads use `networkMode: "always"` because they are local SQLite
or durable-queue reads:

- account, folder, message, thread, starred, attachment and unread-count
  queries;
- basic local search and pending remote-write summaries/lists;
- Outbox summaries;
- cached Gmail connection states, download-failure records and cached Gmail
  labels;
- message-label metadata when the local-label capability is enabled.

Explicit retry, reconnect, Gmail label refresh and other provider actions keep
their existing authorization and remote-service boundaries. Advanced search
continues to use the online query policy. Contacts and local-label compatibility
commands remain startup stubs behind unavailable-feature gates and are not
claimed as implemented by this validation.

## Automated validation

- Targeted offline/settings regression: **7 files / 76 tests passed**.
- Full `bun run check`: Go tests/vet/race, MyGO vet, bindings, TypeScript,
  **161 Vitest files / 1142 tests**, frontend build and all five configured
  cross-compilation targets passed.
- `git diff --check` passed.

The test cases cover uncached local reads and explicit invalidation while
`onlineManager` is false, account switching without stale results, durable
mail actions, local Outbox refresh and enqueue, account connection state,
download-failure retry, and cached Gmail-label refresh. Expected i18n/privacy
listener warnings remain test-environment diagnostics only.

## Packaged desktop validation

An ad-hoc signed macOS arm64 app was built in a disposable output directory and
strict code-sign verification was attempted. The packaged WebView selftest
completed **54/54 checks**, including:

- offline browser state applied and restored in the real WebView;
- list star persistence while synthetic Gmail was stalled;
- uncached Starred and basic Search local reads while offline;
- Outbox local refresh and preservation of an unknown send without a remote
  submission;
- the previous Gmail sync, drafts, labels, account, privacy, lifecycle and
  outbox checks.

The ordinary non-selftest empty profile stayed alive for five seconds and
exited cleanly with code 0 after SIGTERM. The package used a disposable
profile, synthetic Gmail fixture and test keyring; it did not contact Google,
use the system keyring or prove Windows/Linux runtime behavior.

## Remaining acceptance

Live Google OAuth/refresh, real recipient delivery, system-keyring behavior and
Windows/Linux runtime acceptance remain manual work. Cross-compilation is not
runtime evidence. Production IMAP/SMTP remains the separate M2 scope.
