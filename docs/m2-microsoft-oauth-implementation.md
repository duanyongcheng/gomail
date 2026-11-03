# M2 Microsoft OAuth and account authorization

Implemented 2026-11-02 in `internal/oauth/microsoft.go` and
`internal/app/microsoft_accounts.go`, with dedicated synthetic tests in each
package. This report covers these modules, not the entire M2 provider acceptance.

## Implemented behavior

- Native public-client PKCE authorization against the public-cloud v2 authority.
  Tenant keywords or nonzero GUIDs are validated against explicit application
  audience policy. Arbitrary authorities, tenant path injection and mismatched
  token endpoints are rejected. No client secret or Google authorization
  parameters are submitted.
- One-shot loopback callback with random state, constant-time comparison, PKCE
  S256, host/method/query bounds, denial, cancellation and timeout handling.
- Separate Graph and Exchange scopes and grants. Graph scope shorthand returned
  by Microsoft is recognized. IMAP and SMTP access are checked independently.
- Explicit resource-scoped refresh under a credential-wide mutex. Missing scope
  and refresh-token fields preserve prior provenance; explicit scope reductions
  are retained without granting missing permission. A healthy IMAP grant remains
  usable when SMTP permission is missing.
- Credential persistence happens before exposing a new token. The callback gets
  detached observed/new snapshots and must encrypted-CAS them. CAS/persistence
  failure fences the source until restore or reconnect, including subsequent
  callers. Shared refresh-family rotation updates matching refresh credentials
  without replacing other resources' access tokens or scope provenance.
- HTTP clients inherit the supplied transport, deny redirects and apply a timeout.
  Response bodies and fields are bounded; failures omit server descriptions,
  tokens and raw protocol data.
- Stable identity comes from authorized Graph `/me`, not typed addresses or JWT
  decoding. The Exchange authorization is independently verified by explicitly
  refreshing its grant for Graph User.Read and comparing `/me`; rotation from
  this identity check is retained before encrypted storage.
- Exact XOAUTH2 SASL initial response and mechanism, with credential injection
  checks, bounded single challenge and no password fallback in this module.
  TLS-before-AUTH remains the protocol transport caller's responsibility.
- Typed `MicrosoftService.Authorize`, `Status` and `Cancel` implement actual
  account creation/resource authorization. They resolve global/account proxy
  policy through `mailnet`, encrypt the full production protocol config, enforce
  same stable identity on replacement, CAS existing credentials, construct lazy
  providers and fence the previous generation. Failed authorization, cancellation
  or CAS does not replace stored account data. Authorization does not start sync.
- Safe status DTOs expose client/tenant/audience, selected policies and individual
  resource grant availability without credentials. Successful account changes
  emit the account event after account locks have been released.

## APIs and integration obligations

`NewMicrosoftConfig` accepts audience values `single_tenant`, `organizations`,
`personal`, or `all`. These reflect the application's actual native-client
registration; they are not inferred from the tenant.

`NewMicrosoftTokens(policy, client, initial, persist)` performs no network work.
Its `Token(ctx, resource, requiredScopes...)` requires explicit protocol scopes
for independent IMAP/SMTP policy. The persistence callback must use the account
generation guard and encrypted configuration mutation/CAS. Account restore only
reconstructs lazy sources. Never return `MicrosoftCredentials`, `MicrosoftGrant`,
or `ProtocolConfig` through UI bindings.

The application registry supplies the Outlook factory, Graph implementation and
protocol factories. This slice does not claim UI bindings, protocol execution,
Graph synchronization/writes or full-package verification independently of those
integration owners. The common account service must reject unverified Outlook
account creation; validated MicrosoftService is the identity-authorized path.

## Verification evidence

- `GOCACHE=/private/tmp/pebble-ms-go-cache go test ./internal/app -run MicrosoftService -count=1`
  passed locally (1.137 seconds), using disposable SQLite/keyring fixtures and
  an in-process synthetic HTTP transport. Tests cover encrypted add, verified
  identity, safe status, local-only restore, duplicate add, wrong mailbox,
  cancellation, concurrent credential replacement and Exchange-only IMAP policy.
- Parent Microsoft coordinator executed the final host OAuth race suite after
  the Exchange identity and shared-refresh-family additions: `go test -race
  ./internal/oauth` passed (2.055 seconds), including the local HTTPS/PKCE
  fixtures. The accompanying Graph race suite passed (3.569 seconds); Graph is
  owned and reported separately by that coordinator.
- The worker environment denied the default Go cache and loopback listener bind;
  an isolated `/private/tmp` cache allowed compilation/service tests. OAuth TLS
  fixtures remain runnable by the parent host test runner. No test was skipped
  or marked successful because a listener was blocked.
- After the separately owned provider modules finished their concurrent compile
  changes, `GOCACHE=/private/tmp/pebble-ms-go-cache go test -race ./internal/app -run MicrosoftService -count=1`
  passed locally (3.445 seconds). The listener-free OAuth policy/XOAUTH2 check
  also passed (0.417 seconds). These are focused checks, not the full gate.

The OAuth fixture suite covers tenant/audience rejection, PKCE and public-client
form, state mismatch, deny/cancel, actual granted scopes, resource-specific
refresh, concurrent refresh, encrypted-CAS callback failure, reopen without
refresh, omitted and reduced scopes, independent protocol policy, redirect
rejection, invalid/oversized responses, identity and exact XOAUTH2 framing.

## Explicit remaining acceptance

No real account, profile, keyring, live Microsoft endpoint or mailbox was accessed.
No normal application was launched. Client registration/public-client policy,
dedicated personal and organizational consent, tenant SMTP AUTH policy, real
expiry/refresh and independent IMAP/SMTP delivery remain manual acceptance work.
Successful SMTP/Graph submission is not delivery evidence. Full `bun run check`,
packaged signed build and isolated desktop selftest belong to the combined gate;
this module report does not assert they passed.
