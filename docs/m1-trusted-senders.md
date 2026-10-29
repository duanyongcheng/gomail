# M1 persisted sender trust and privacy policy

The trusted-sender commands now use the legacy `trusted_senders` table rather
than startup stubs. No migration is required: schema remains v28 and the Rust
v20 schema oracle is unchanged. The 145 invoke inventory now has 49 implemented
commands, including list/set/remove/check sender trust.

## User flow and meaning

A message's privacy banner offers **Load images** for that message only,
**Trust images** to remember images-only permission, and **Trust sender** for full
trust. Conversation bubbles offer the same controls. Full trust asks for explicit
confirmation explaining that remote content can reveal IP address/read timing.
Trust is a preference for the claimed From address, **not sender authentication**.

Settings → Privacy lets you choose an account (also from combined-inbox mode),
add/update an exact address and level, or revoke trust. It shows read/save/removal
errors and loading state. Duplicate actions are disabled while saving. A failed
save does not change rendering policy; a failed removal retains the list entry.

Addresses are trimmed and ASCII case-folded, with a single plain mailbox required.
Display names, lists, wildcards, domains alone, controls and Unicode mailboxes
are rejected. This version does not support domain trust rules or internationalized
mailbox normalization. Records contain ordinary local address metadata, not
credentials, and do not contact Gmail or require a new OAuth scope.

## Effective backend policy

`MessageService.Rendered` resolves the effective mode using the **message's own
account and From address** before passing HTML to the sanitizer:

| Request / persisted record | Effective behavior |
|---|---|
| Strict, no trust | Block remote images and trackers |
| LoadOnce, no trust | Allow ordinary HTTPS images; block trackers |
| Strict or LoadOnce, images trust | Allow ordinary images; block trackers |
| Strict or LoadOnce, all trust | Allow images and trackers |
| Matching TrustedSender object, stored images | Images-only; cannot escalate |
| Matching TrustedSender object, stored all | Full trust |
| Unstored or different-sender TrustedSender object | Strict |
| Explicit Off | Off regardless of trust records |

Persisted exceptions therefore override the normal Strict/Relaxed default, as in
the reference implementation. Remove the trust record to restore default blocking.
Off is a separate explicit choice. Full trust never permits scripts, event handlers,
unsafe URLs or CSS remote loading, and does not enable external stylesheets (Off
alone allows sanitized HTTPS stylesheet links). CID authorization is unchanged.

A trust database failure rejects protected-mode rendering rather than returning
unchecked permissive HTML. The UI cannot grant full trust by merely submitting
`{"TrustedSender":"someone@example.invalid"}`. This closes a gap in the initial
privacy slice, which parsed that object but had not yet checked persisted trust.

## Persistence and invalidation

Set validates account/type/address and atomically replaces case/space variants.
Old conflicting variants resolve to the more restrictive images-only level until
an explicit update chooses a new level. Removal clears equivalent variants for
that account only. Account deletion cascades trust records; reopening a profile
retains them. No keyring or network request is needed for trust CRUD.

After successful writes the backend emits `mail:trusted-senders-changed` with
`accountId,email`. Ordinary readers, expanded conversation bubbles and settings
re-read matching data. Default-mode changes dispatch a same-window privacy event.
Render request generations are invalidated immediately; old permissive responses
cannot overwrite newer revoked/strict results, including initial message loads.
The reader clears old rendered HTML while awaiting the new render. Resources
already fetched before revocation cannot be retroactively unfetched.

The banner no longer grants an in-memory full-trust override after success: it
refreshes the current requested mode and lets the backend apply the saved level.
A temporary LoadOnce choice still applies to that message after trust removal;
reopening it returns to the profile's default. Off similarly remains explicit.

## Regression lessons

The original renderer tested sanitizer modes in isolation, not whether callers
were authorized to request trusted mode. Service-level tests now cover the full
request/account/sender/persisted-level matrix, failed trust reads and revocation.
Frontend tests cover a revoked policy racing an older HTML response, rather than
only checking that a button changed a local enum.

See [validation](m1-trusted-senders-validation.md). No live remote image/tracker
was fetched during this slice's tests. Gmail cloud drafts and remaining account
identity/authorization polish are separate M1 work.
