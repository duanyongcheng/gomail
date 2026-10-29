# M1 Gmail account access and reconnection

Settings → Accounts → **Gmail connection** shows local credential availability,
temporary connection trouble, expired/revoked authorization, missing mail
permissions, unavailable encrypted credentials, configuration problems, and an
in-progress Google sign-in. Opening settings reads the local profile only.
“Local credentials available” is not a fresh network check.

## Restore and classification

Startup restores accounts independently. An unreadable token or mismatched OAuth
client in one account no longer prevents later accounts from installing their
providers. A locked/missing profile key affects encrypted credentials for all
accounts; ciphertext, cached mail, local drafts and pending work are retained.
An empty profile does not read the keyring or contact Gmail.

Token refresh distinguishes `invalid_grant`/`invalid_token` from a temporary
network/server error, and `invalid_client`/`unauthorized_client` from mailbox
permission. A final API 401 requires reconnection. A 403 requires mail permission
only when Gmail explicitly reports `insufficientPermissions` or
`ACCESS_TOKEN_SCOPE_INSUFFICIENT`; quota and unrelated policy errors are not
mistaken for a revoked grant.

Confirmed access failures pause that account's automatic sync and exclude its
pending writes/outbox items from claims. Other accounts continue. An already
claimed operation rejected for authorization is released without consuming a
retry attempt. Ordinary network and quota failures keep their existing backoff.
Terminal access state is stored at `accounts.sync_state.gmail_auth_status` so a
restart does not repeatedly consume the queue's attempt budget. If this state
cannot be saved, the account stays paused in memory and workers retry the local
state write. No schema change is needed; v28 and the Rust v20 oracle are retained.

## Reconnect an existing mailbox

Choose **Reconnect** beside the existing Gmail account. Google sign-in opens in
the system browser with PKCE, the existing one-shot loopback callback, and offline
consent. **Cancel sign-in** stops the pending flow. The page/app context also
cancels it on shutdown. Only one Google authorization flow can run at a time;
other accounts remain usable while the browser is open.

Before replacing credentials, the backend verifies the newly issued access
token's Gmail primary address against the original mailbox and requires an actual
returned `gmail.modify` or full-mail grant. Choosing another Google account,
missing permission, denied consent, cancellation, or failed profile verification
keeps the original credentials. A callback received after cancellation or account
deletion cannot recreate the account.

The replacement updates only encrypted authentication and access state. Account
ID, email, sender name, color, label, ordering, cached messages, sync checkpoints,
local drafts, attachment references and queues remain intact. Credential storage
uses compare-and-swap; an older token refresher cannot overwrite the new grant.
Provider generations reject old requests/results after credential replacement.

Successful reconnection restarts syncing according to the profile's applied
preference and wakes queued work. Manual mail-sync remains manual. Previously
queued sends remain authorized work and can resume independently of the mail-sync
preference. **Outcome-unknown sends remain unknown and are never automatically
resent.** Check Sent remains the read-only reconciliation action.

## Interfaces and compatibility

Generated `AccountService.connections/reconnect/cancelReconnect` methods and
`mail:account-changed {accountId}` serve the frontend. Existing 145 invoke commands
and their signatures are unchanged. The account event refreshes capabilities and
connection status; settings also refresh local status every three seconds.

New token envelopes retain the actual granted scope because Go's `oauth2.Token`
JSON does not serialize `Extra("scope")`. Refresh preserves a missing scope or
refresh token from the previous grant. Old encrypted envelopes without scope
metadata remain readable and rely on actual API authorization responses; the
isolated read-only protocol prototype retains its read-only semantics.

## Boundaries

This slice does not edit account identity/profile settings, migrate old profiles,
revoke Google's grant, add aliases, change proxies, or implement cloud drafts.
Cancelling a browser flow does not close the user's browser window. Reconnection
requires the original profile key; it cannot recover a lost encryption key.
User-facing diagnostic strings are static and do not contain token/HTTP bodies.
Real Google consent and other OS runtime checks remain manual; automated checks
use local fixtures only. See [validation](m1-account-reauth-validation.md).
