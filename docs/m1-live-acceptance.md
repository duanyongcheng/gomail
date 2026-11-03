# M1 dedicated-account live acceptance

Status reconciled on November 2, 2026. The user reports basic Gmail send/reply
and corrected address-name display verified. The address-name fix is independent
commit `d4fce6e`. This is bounded user confirmation, not a rerun by this task.
All other live rows below are pending. M1 live acceptance remains open; M2
implementation and manual acceptance are not completed by this document.

The [daily-use plan](gmail-daily-use-plan.md) tracks scope. Prior automated
evidence is recorded in [address-name validation](address-name-decoding-validation.md),
[account reconnect validation](m1-account-reauth-validation.md),
[offline workflow validation](m1-offline-local-workflows-validation.md),
[Gmail completion validation](m1-gmail-completion-validation.md) and
[review validation](m1-review-fixes-validation.md). Fixture success, synthetic
desktop checks, compilation and packaging do not verify real OAuth/keyring or
recipient behavior. Those reports describe their own dates and environments.

## Prepare an isolated run

1. The operator explicitly chooses dedicated Gmail accounts A and B plus
   controlled To/Cc/Bcc recipient mailboxes, and approves the synthetic messages
   and any grant revocation before execution. This documentation task authorizes
   no additional real-mail writes. Enter credentials and consent only in local
   UI/browser dialogs; never put credentials, tokens or recovery codes in chat.
2. Use a disposable OS test user or VM with its own system keyring. A different
   `PEBBLE_PROFILE_DIR` alone does **not** isolate the keyring: the current service
   is `dev.bary.pebble.go` and account is `master-dek`. Do not inspect, lock,
   delete, replace or migrate the normal user's keyring/profile. Confirm local
   recovery access for the disposable environment before keyring tests.
3. In that isolated OS session, use the intended platform package and an empty
   profile directory. Record app commit/version, OS/architecture, package path,
   test-account aliases A/B, controlled recipient aliases and local time zone.
   Keep `PEBBLE_SELFTEST` unset: it substitutes synthetic accounts/keyring.
   On macOS, the following launches the existing package without building it;
   execute only after completing steps 1 and 2. Keep this terminal open so the
   same profile can be used after quitting:

   ```sh
   mktemp -d "${TMPDIR:-/tmp}/pebble-m1-live.XXXXXX"
   # Set this to the directory printed above, within the disposable OS session.
   PEBBLE_ACCEPTANCE_PROFILE=/absolute/path/from/mktemp
   PEBBLE_ACCEPTANCE_APP='/absolute/path/to/Pebble Go.app/Contents/MacOS/Pebble Go'
   env -u PEBBLE_SELFTEST PEBBLE_PROFILE_DIR="$PEBBLE_ACCEPTANCE_PROFILE" "$PEBBLE_ACCEPTANCE_APP"
   ```

   For other platforms, use their actual package executable with the same
   profile environment variable and isolated OS credentials store. Do not count
   cross-compilation as a platform launch. Do not launch against a normal running
   app; use the separate OS session to avoid single-instance handoff.
4. Add only A/B through Gmail consent. Use a unique synthetic subject marker
   per approved send, a harmless small attachment with known filename/bytes and
   synthetic names/body. Record a baseline of local account/draft/queue state
   through UI before each scenario. Keep artifacts local and sanitized.

## Live checklist

Execute each row independently, record actual observations and stop the affected
scenario on unexpected identity changes, loss of local data or duplicate mail.
Restart means fully quit and relaunch the same executable/profile, not merely
close and reopen the window. Use normal quit for live preservation tests;
submission crashes and lost-response faults belong in fixtures.

| ID | Procedure | Required observations / evidence | Status |
| --- | --- | --- | --- |
| M1-01 | Review the already supplied basic Gmail send/reply and corrected address-name result. | User-reported confirmation only; display-name fix `d4fce6e`. No broader recipient/platform inference. | User-reported verified; not rerun here |
| M1-02 | In A, complete consent, sync approved samples, quit, wait beyond the actual access-token expiry, relaunch and explicitly sync. If separately approved, send one synthetic message. Do not inspect/edit token files or substitute a shortened fake expiry. | Actual elapsed expiry evidence and successful refresh without repeated consent; same mailbox/cache retained across another restart. If expiry cannot be established safely, keep pending. Queue acknowledgement is not proof of send/delivery. | Pending live |
| M1-03a | Record A's local drafts/queue and manual-sync preference. Revoke only A's disposable grant in Google's account UI; explicitly sync to observe an access error. Choose Reconnect and consent as the same primary A mailbox, then restart. | Actionable access state; same account identity, cached mail, drafts and queued intent retained; eligible work can resume, manual sync stays manual. No account duplication or credential output. | Pending live |
| M1-03b | Repeat the revoked-grant setup in A; choose Reconnect, then cancel in the app. Separately deny consent in the browser. | Each cancellation/denial leaves A and its cache/drafts/queue intact, ends reconnecting state and allows a later explicit reconnect. | Pending live |
| M1-03c | With A requiring reconnect, select dedicated B in the browser instead. | Mailbox mismatch is visibly rejected; A is not rebound to B or duplicated; original local state is preserved. Complete a later explicit reconnect as A to recover. | Pending live |
| M1-04a | In the disposable OS environment only, quit, lock its test keyring using platform UI, relaunch the populated test profile and inspect account status. Unlock through OS UI and relaunch. | Actionable credentials-unavailable behavior, no unsolicited login or replacement key; encrypted profile bytes retained and account/draft content recovers after unlock. Record any OS auto-unlock behavior; if the lock cannot be held, this row is inconclusive. | Pending live |
| M1-04b | In a recoverable disposable VM snapshot only, make its test credentials store unavailable through supported platform controls, then launch the populated profile. Restore the store/snapshot afterward. Do not delete a DEK to simulate unavailability. | Existing encrypted data preserved, bounded local error, no replacement key or automatic consent; restored original store opens the same data. If safe isolation/recovery is unavailable, leave pending. | Pending live |
| M1-05 | Cache approved samples, go offline, open cached mail, mark read/unread, compose and save a local draft with attachment; queue one approved synthetic message offline. Fully quit/relaunch while still offline, inspect and edit local work, then reconnect. | Local metadata/read state, exact draft text/attachment and durable queue survive restart. Eligible queued work submits once; controlled recipient and Outbox evidence agree. Manual mailbox sync preference does not stop explicitly queued sends. Never manufacture an unknown live send. | Pending live |
| M1-06a | Send approved synthetic mail separately to To, To+Cc and To+Cc+Bcc controlled recipients. Include Unicode display names and a small attachment with a Unicode filename. | Each intended recipient independently confirms receipt, accurate decoded addresses/names, filename and attachment byte/hash match. To/Cc recipients cannot see Bcc in received headers; Bcc recipient receives the mail. Record sanitized evidence, not raw MIME. | Pending expanded matrix |
| M1-06b | Reply and Reply All to controlled samples with To/Cc and Unicode names; forward one attachment sample. Exercise locally saved and explicitly imported Gmail drafts separately, preserving Bcc/reply context. | Correct recipient set, subject, reply threading/References/In-Reply-To and attachment bytes/names in controlled recipients' UI/header inspection. No accidental reply to self or exposure of a hidden recipient. Record each variant separately; basic reply confirmation does not close this row. | Pending expanded matrix |
| M1-07 | Add A/B, set manual mailbox sync, cache samples and local drafts separately, quit/relaunch. Observe without clicking sync, then explicitly sync A followed by B. Optionally revoke A's dedicated grant and repeat B sync. | Saved manual preference applied before automatic polling; restore performs no mailbox requests, browser consent or unsolicited refresh. Explicit sync updates only its account; drafts/queue/state remain isolated and B works while A is blocked. Use sanitized request-count instrumentation if available; UI silence alone cannot prove no network. | Pending live |
| M1-08 | Repeat launch, keyring persistence, restart and bounded-error checks for each claimed supported platform package in its own disposable OS session. | Installed package/OS/architecture, launch/reopen and actual system-keyring evidence recorded separately for macOS, Windows and Linux. Passing one platform does not close the others. | Pending platform acceptance |

Unknown sends, crash after submission, failed receipt persistence, refresh CAS
races and deliberate network loss during writes must use isolated fixtures.
Never force these against Gmail. An existing unknown item must remain unknown
across restart/reconnect: only an explicit read-only Check Sent with a unique
verified match can resolve it. No match is not permission to resend. Do not
dismiss its evidence or reset/requeue it to make a live row pass.

## Record and close a row

For each attempted row, retain a sanitized record using this template. Keep
recipient addresses pseudonymized and never attach tokens, keyring exports,
profile databases, raw headers/MIME or unrestricted logs.

```text
Row / variant:
Run timestamp and time zone:
Operator confirmation / approved accounts and synthetic messages:
App commit/version, package, OS/architecture:
Disposable OS/keyring and profile alias:
Precondition (including elapsed token expiry where relevant):
Actions and observed status:
Sanitized UI/recipient evidence and local artifact reference:
Result: pending / pass / fail / inconclusive
Remaining limitation and recovery performed:
```

Only mark a row passed after recording its own evidence or explicit user
confirmation for that exact behavior. Keep unexecuted/inconclusive variants
pending. This task performs documentation and targeted synthetic regression
checks only; all rows except M1-01 remain open. M2 has its own implementation and
acceptance gates and cannot be closed by this checklist.
