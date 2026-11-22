# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

General desktop email users handling daily office correspondence and personal communication across one or more email accounts. They expect an approachable, intuitive interface that requires no technical setup knowledge during everyday use, while supporting comfortable long-form email reading and light work organization.

## Product Purpose

Pebble Go is a local-first desktop email client built with Go, MyGO WebView, and React. It gives users a reliable, private, offline-capable desktop workspace to read, search, organize, compose, and send mail across Gmail, IMAP/SMTP, POP3, and Microsoft Outlook/Graph accounts without relying on webmail tabs or cloud middleware. Success means everyday mail workflows—scanning accounts and folders, reading long messages and threads, writing replies and drafts, and understanding sync/send state—feel calm, clear, and trustworthy.

## Positioning

Unlike browser-wrapped webmail or cloud-indexed email services that upload mailbox credentials to third-party servers, Pebble Go keeps storage (pure-Go SQLite with WAL and FTS5), cryptography, HTML sanitization, durable outbox/pending-write queues, and encrypted local drafts entirely on the user's machine while still offering an approachable everyday desktop email experience.

## Operating Context

- Cross-platform desktop WebView application (macOS WebKit, Linux WebKitGTK, Windows WebView2) running in a resizable desktop window with custom title bar, sidebar navigation, main workspace, status bar, command palette, and modal dialogs.
- Multi-account mail reading and triage across unified ("All accounts") and per-account mailboxes, with message list / thread view toggle, message detail reader, starred view, local FTS5 search, compose editor (rich text, Markdown, HTML, attachments, local drafts, Gmail cloud draft copies, durable outbox), and multi-tab settings.
- Bilingual Chinese (`zh`) and English (`en`) user interface; users may switch languages and color modes (`light`, `dark`, `system`) at runtime.
- Offline-first operation: cached mailboxes, search, drafts, and local settings remain usable offline; remote actions (sending, server label/folder changes, pending remote writes) clearly communicate queued, retrying, or uncertain states.

## Capabilities and Constraints

- **Implemented core workflows:** Multi-account Gmail (OAuth PKCE), IMAP/SMTP, POP3, and Microsoft Outlook/Graph; local SQLite + FTS5 search; message & thread views; HTML privacy sanitization (`Strict`, `LoadOnce`, `Off`, `TrustedSender`) and per-account trusted senders; inline CID images and attachment Download / Save As; encrypted local drafts, explicit Gmail cloud draft copy management, and durable Outbox; server label/folder management; per-account proxy/network routing; keyboard shortcuts and command palette; message detail reading templates.
- **Unified interface theme architecture (confirmed):** Replace the legacy 5-interface-theme switcher (`paper`, `clay`, `glass`, `editorial`, `minimal`) with one unified design system while preserving `light` / `dark` / `system` color modes and independent message-detail reading templates.
- **Guarded / upcoming views (confirmed):** Views and settings tabs currently gated by feature availability (`Kanban`, `Contacts`, `Snoozed`, `Rules`, `AI`, `Translation`, `Cloud Sync / Backup`, plus disabled controls in `General` / `SearchFilters`) must also be redesigned under the same visual system while strictly preserving their availability guards (`isFeatureAvailable`, `isViewAvailable`, `isSettingsTabAvailable`) and truthful unavailable messaging.
- **Local-only runtime constraint:** Must not rely on external network CDNs for fonts, icons, or assets at runtime; everything ships locally in the bundle or uses OS-available typography.
- **Existing behavior and test integrity:** Preserve all Go/MyGO service bindings (`src/mygo.ts`), IPC commands, accessibility semantics, keyboard shortcuts, bilingual locale keys, and data durability/safety prompts (privacy banners, outbox unknown-outcome warnings, destructive delete confirmations).

## Brand Commitments

- Product name: **Pebble** / **Pebble Go** (do not rename).
- Existing app icon asset: `src/assets/app-icon.png` / `resources/icon.png`.
- Voice: Plain, calm, honest, and non-technical in everyday surfaces; explicit and transparent when communicating privacy, offline, or uncertain remote-write states.

## Evidence on Hand

- Live React + TypeScript application source under `src/` (layout shell, sidebar, inbox/thread/reader, starred, search, compose/drafts/outbox, settings tabs, command palette, dialogs, guarded M3–M5 views) and Go backend under `internal/`.
- Bilingual locale catalogs in `src/locales/zh.json` and `src/locales/en.json`.
- App icon assets in `src/assets/app-icon.png` and `resources/icon.png`.
- No external marketing testimonials, benchmarks, or third-party endorsements exist in the repository; future work must not fabricate them.

## Product Principles

1. **Approachable over arcane:** Everyday reading, writing, and account switching must feel immediately clear to general desktop email users without requiring power-user knowledge.
2. **Reading comfort first:** Long-form messages, multi-message threads, and bilingual Chinese/English copy deserve generous breathing room, calm hierarchy, and effortless scannability.
3. **Honest local-first state:** Sync progress, offline status, privacy blocking, queued remote writes, and uncertain send outcomes must be clear and trustworthy without causing alarm fatigue.
4. **One coherent workspace:** Every view—from inbox and compose to settings and guarded upcoming tabs—belongs to one unified desktop application rather than a patchwork of disparate styles.

## Accessibility & Inclusion

- Full keyboard navigability (existing shortcut system, command palette, `focus-visible` outlines, roving tablists in settings) and semantic ARIA landmarks/roles (`role="tablist"`, `role="dialog"`, `aria-current`, `aria-expanded`, `role="alert"` / `role="status"`).
- Readable contrast across both light and dark modes in Chinese and English text.
