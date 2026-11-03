# M2 packaged synthetic provider fixture

Implemented on 2026-11-02. This report describes synthetic verification only.
It is not live account, delivery, system keyring, OAuth consent or packaged
desktop execution evidence.

## Files

- `selftest_m2.go`: disposable, headless-capable production-service scenario and
  loopback-only TLS IMAP, SMTP and POP3 fixture servers.
- `selftest_m2_test.go`: executes the same scenario without launching MyGO and
  requires every named assertion to pass.
- `selftest.go`: initializes the seven M2 runtime assertions and calls the M2
  scenario before the existing quit-on-close desktop check.

The original seven backend checks do not use the window's UI profile/runtime. The
scenario creates a fresh temporary directory, SQLite database, runtime and
in-memory keyring, and removes that directory when finished. All listeners bind
`127.0.0.1:0`; a generated short-lived certificate is explicitly trusted through
the normal per-endpoint root CA configuration. Verification is not disabled.
There are no dependency changes, commits, external requests, real account
credentials, `.env` reads, auth-file reads or system keyring calls in this code.

## Executed behavior

1. `AccountService.Test` uses the actual standard provider factories and verified
   TLS IMAP/SMTP and POP3 connections. `AccountService.Add` then persists both
   accounts through the production encrypted configuration path. The stored
   credential envelope has the expected encrypted format and does not expose
   the synthetic password.
2. `SyncService.Sync` caches exactly two selected IMAP folders and excludes the
   third fixture folder. POP3 retrieves one stable UIDL message. Provider
   capability output disables POP3 send, remote flags and folder management
   when SMTP is not configured.
3. The database closes and reopens with the same in-memory key. A new runtime
   restores both accounts and lists their capabilities without creating any
   server connection. Repeated sync preserves cached local IDs and retrieves no
   duplicate POP3 UIDL. Appending another IMAP mail then exercises incremental
   sync to a three-message IMAP cache.
4. `OutboxService.Queue` freezes an IMAP account's SMTP submission with To and
   Bcc recipients. The actual Outbox worker sends once through the production
   SMTP transport and persists `sent` with an empty RemoteID and one attempt.
   The fixture verifies both envelope recipients, one DATA submission, and no
   Bcc header or hidden-recipient address in MIME.

This scenario caught a production integration defect: `AccountService.Test`
passed an empty account ID to the IMAP constructor, causing new-account
connection tests to fail before dialing. The integration coordinator fixed the
service to use a nonpersistent test identity; this fixture passes after that
fix and does not bypass the constructor.

## Verification

- `CGO_ENABLED=0 go test . -run TestPackagedM2SyntheticProviders -count=1 -v`
  passed, including all seven named assertions.
- `CGO_ENABLED=0 go build -o /tmp/pebble-m2-fixture-build .` passed.
- `go test -race . -run TestPackagedM2SyntheticProviders -count=1` passed.
- `git diff --check -- selftest.go selftest_m2.go selftest_m2_test.go` passed.
- The integration coordinator owns the combined quality gate, signed build,
  isolated packaged desktop launch and its final `runtime.json` evidence.

## Limits and manual acceptance

This packaged scenario covers implicit TLS protocol accounts. STARTTLS/STLS,
UIDVALIDITY reset, paging/fault matrices, durable remote mutations, Microsoft
OAuth/XOAUTH2 and Graph have their respective provider-level synthetic suites;
they are not asserted by these seven packaged checks. This backend scenario invokes
typed Go application services. A separate `selftest_m2_ui.go` scenario now drives
the actual React protocol form, settings, manual sync, message opening and
SMTP/Bcc compose controls in the supplied window, with seven separately named
WebView checks. It verifies persisted effects and synthetic server receipts;
no injected JavaScript calls services directly. See `m2-review-validation.md`
for execution evidence and limits. SMTP acceptance is server acceptance, not recipient
delivery. Live credentials/client registration, tenant consent, system-keyring
behavior and controlled recipient acceptance remain separate manual work.
