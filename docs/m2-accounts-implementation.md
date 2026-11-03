# M2 encrypted accounts and atomic provider state

Implementation report, 2026-11-02. This report covers the accounts worker's files and synthetic checks; the integration coordinator owns the combined provider, frontend, full quality gate and packaged runtime result. No real account, profile, keyring or mail service was accessed.

## Implemented behavior

- Versioned `domain.ProtocolConfig` holds independent IMAP, SMTP and POP3 endpoints/authentication, selected folders, account proxy precedence, explicit root CA trust and Microsoft resource grants. It is encrypted using the existing AES-GCM envelope, purpose `accounts.auth_data` and account ID AAD. Existing Gmail token records and ciphertext format are unchanged.
- `AccountService.Add` preserves the legacy flattened request while accepting a write-only structured config. It creates account and ciphertext in one statement, rejects duplicate mailbox/provider identities without upserting, and rejects unverified Microsoft grants; Microsoft creation uses the separate validated OAuth service. Save is local. Connection test is a separate explicit action.
- `ProtocolSettings` returns safe endpoints, credential-presence booleans and ciphertext revision, never passwords or resource tokens. `SaveProtocol` preserves blank password fields and existing Microsoft grants. It validates before replacing the working account, stops and joins scheduler work, holds durable-write/Outbox locks, and CAS-replaces the observed ciphertext while preserving mailbox identity, cache, drafts and send state. Stale revisions fail. Replacement invalidates previous provider generations.
- `Test` and `TestStored` call the provider's actual connection-test interface with a bounded context. They do not sync, save account data or persist token changes, and sanitize provider failures. Disabled certificate verification/plaintext requests are rejected; supplied CA certificates use `mailnet` verified trust policy.
- Restore decrypts local account data and constructs registered lazy providers. It does not call Test, Sync, token refresh or browser authorization. Wrong-key/corrupt records fail per account while healthy accounts restore. Missing keyrings preserve ciphertext. Runtime connection object identity fences stale requests and refresh persistence.
- `ProviderContext.UpdateConfig` serializes mutations of the latest encrypted configuration and CAS-saves before returning. Independent resource refresh can update only its grant while retaining other resource changes. `SaveConfig` remains available for deliberate full-record replacement. Losing CAS never returns a success to a token publisher.
- Global proxy config persists locally. Inherit, disabled and custom policy uses `mailnet.ResolveProxy`; settings replacement quiesces jobs and durable writes, rebuilds lazy generations, then resumes only the applied scheduler policy. Gmail authorization, profile, token refresh and API requests now share the explicit routed HTTP transport. Browser routing remains external to app proxy control.
- Go-only v32 adds account-scoped provider checkpoints/mappings and global network settings, plus unique protocol mailbox identity. `Snapshot` atomically commits protocol checkpoints, mappings, mapping-scope clears, membership tombstones and folder removals with message cache/FTS and existing local-intent overlays. Folder upsert now enforces owner/parent isolation and stores hierarchy. Legacy v1-v20 migrations and oracle data are unchanged.
- Bounded repository APIs support remote-ID prefix pages and provider mapping pages. Graph's missing-mapping reconciliation helper is owned and tested by the Microsoft worker.

## Shared API

`Runtime.RegisterProviderFactory(provider, ProviderFactory)` installs constructors before restore. Factory receives account, decrypted config, store, effective dialer, generation guard and encrypted configuration mutation callbacks. Constructors must be lazy and must not launch background jobs. Root owns concrete provider registration and scheduler/IDLE integration.

`store.ProviderCheckpoint(ctx, account, provider, scope)` returns opaque bytes; missing state returns `sql.ErrNoRows`. `Snapshot.ProviderCheckpoints` stores those bytes atomically. `ProviderMappings`, `ProviderMappingsPage` and `RemoteIDsPage` provide account-isolated reads. `ProviderMappingScopesCleared`, `RemovedMemberships` and `RemovedFolderIDs` are applied in the same snapshot transaction.

## Files owned or changed

- `internal/domain/protocol_accounts.go`: encrypted configuration record and independent resource grants.
- `internal/app/protocol_accounts.go`: registration, local add/edit/settings/test, config CAS and proxy operations.
- `internal/app/protocol_accounts_test.go`: encrypted persistence, duplicate identity, edit/CAS, stale generation/refresh, lazy restore, wrong-key isolation, explicit testing and proxy cases.
- `internal/app/account_access.go`: mixed-provider lazy restore, safe connection states, Gmail explicit proxy propagation and nil-keyring preservation.
- `internal/app/services.go`: runtime registration/network fields and provider close on deletion. Other integration edits in this shared file belong to the coordinator.
- `internal/store/migrations/032.sql`, `migrations.go`, `store.go`: Go-only v32 schema.
- `internal/store/provider_state.go`, `provider_state_test.go`: atomic checkpoint/mapping/settings APIs and fault/reopen/migration checks.
- `internal/store/repository.go`: snapshot extensions and folder account/hierarchy validation. Other shared integration edits remain coordinator-owned.
- `internal/store/store_test.go`: exclude only explicit Go-added objects from the independent legacy schema comparison; oracle JSON is untouched.
- `internal/mailnet/policy.go`: JSON field tags only for safe typed API interoperability; network behavior remains the network worker implementation.

## Checks actually run

- `go test ./internal/store ./internal/app ./internal/mailnet`: passed after integration imports became available (app 4.311 s; store cached; mailnet 1.420 s).
- `go test -race ./internal/app ./internal/store ./internal/mailnet`: passed (app 73.790 s; store 55.643 s; mailnet 2.112 s) before the final small configuration-merge/folder checks; final targeted checks cover subsequent account changes.
- `go test -race ./internal/app -run 'TestProtocol'`: passed (5.337 s), including concurrent mutator merge and rejected unverified Microsoft grant.
- `go test -race ./internal/app -run 'TestProtocol|TestGlobalNetworkSettings'`: passed (4.836 s), including persisted global proxy, lazy generation rebuild and proxy restoration.
- `git diff --check`: passed after account changes.
- Store fault fixture aborts checkpoint update and proves both message and checkpoint rollback, then closes/reopens the database and checks persisted mappings/account cascade. Migration fault fixture creates duplicate legacy protocol identities and proves v32 DDL and version fully roll back, leaving v31 and the legacy schema intact.

During concurrent integration, one transient run failed because IMAP `announcements` was being edited; its owner fixed that compilation issue. One new account test initially had an unused local variable; it was corrected before the successful results above.

## Remaining acceptance and limits

No system-keyring, live consent/refresh, real server protocol, real delivery, normal-profile application or packaged desktop acceptance is claimed here. These require the coordinator's isolated synthetic package checks and separately authorized dedicated-account manual acceptance. Provider connection tests and folder/send capabilities depend on the concrete implementations registered by integration; this worker does not mark their independent protocol acceptance complete.

Existing legacy duplicate protocol mailbox identities cause a safe, rolled-back v32 migration failure instead of silently discarding or replacing account records. Such a profile requires explicit local duplicate reconciliation before upgrade. Credentials and cache remain intact.

No dependencies were changed; no commits or remote actions were performed.

## Follow-up: Gmail account-specific proxy

Added `internal/app/account_network.go` and `internal/store/account_network.go` with safe `AccountNetwork(ctx, AccountRequest)` and `SetAccountNetwork(ctx, AccountNetworkRequest{accountId, setting})` methods. Gmail's nonsecret proxy policy is stored additively at `sync_state.account_proxy`, preserving ciphertext, checkpoint and unrelated state; protocol account proxy remains inside its encrypted config.

Gmail installation and reconnect resolve global/account policy for every authorization, token refresh, profile and API client. A known-email repeat login pins that account's route before authorization and rejects selection of a different mailbox. Fresh consent necessarily starts with the global route because mailbox identity is not known yet; if the resulting mailbox already has a non-inherited policy, the flow refuses to replace credentials and instructs the user to use account reconnect. Existing explicit reconnect always knows the account before any request. The OS browser still uses its own routing.

Account and global network setters reject changes during an active authorization exchange, quiesce account workers and durable writes, rebuild lazy provider generations, preserve account access status/credentials and resume only the applied scheduler policy. No migration was required for the additive Gmail setting.

Validation: `go test ./internal/app ./internal/store` passed (4.561 s / 3.155 s); `go test -race ./internal/app ./internal/store -run 'TestGmailAccountNetwork|TestAccountNetwork|TestGlobalNetworkSettings'` passed (2.254 s / 1.815 s); `git diff --check` passed. Synthetic tests check state preservation/account isolation, protocol-account rejection, restore and generation replacement, authorization-flow exclusion, local HTTP requests with account `disabled` bypassing an unreachable global proxy, and an unreachable custom account proxy failing without direct fallback. No real network account or keyring was used.
