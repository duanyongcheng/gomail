# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

General desktop email users handling daily office correspondence and personal communication across one or more email accounts. They expect an approachable, intuitive interface that requires no technical setup knowledge during everyday use, while supporting comfortable long-form email reading and light work organization.

## Product Purpose

GoMail is a local-first desktop email client built with Go, MyGO WebView, and React. It gives users a reliable, private, offline-capable desktop workspace to read, search, organize, compose, and send mail across Gmail, IMAP/SMTP, POP3, and Microsoft Outlook/Graph accounts without relying on webmail tabs or cloud middleware. Success means everyday mail workflows—scanning accounts and folders, reading long messages and threads, writing replies and drafts, and understanding sync/send state—feel calm, clear, and trustworthy.

## Positioning

Unlike browser-wrapped webmail or cloud-indexed email services that upload mailbox credentials to third-party servers, GoMail keeps storage (pure-Go SQLite with WAL and FTS5), AES-256-GCM cryptography (`dev.bary.gomail` OS keyring), HTML sanitization, durable outbox/pending-write queues, and encrypted local drafts entirely on the user's machine while still offering an approachable everyday desktop email experience.

## Operating Context

- Cross-platform desktop WebView application (macOS WebKit, Linux WebKitGTK, Windows WebView2) running in a resizable desktop window with custom title bar, collapsible sidebar navigation, main workspace, bottom status horizon, command palette, and modal dialogs.
- Multi-account mail reading and triage across unified ("All accounts") and per-account mailboxes, with message list / thread view toggle, message detail reader, starred view, local FTS5 search, compose editor (rich text, Markdown, HTML, attachments, encrypted local drafts, Gmail cloud draft copies, durable Outbox), and multi-tab settings.
- Bilingual Chinese (`zh`) and English (`en`) user interface; users may switch languages and color modes (`light`, `dark`, `system`) at runtime.
- Offline-first operation: cached mailboxes, search, drafts, and local account settings remain usable offline; remote actions (sending, server label/folder changes, pending remote writes) clearly communicate queued, retrying, or uncertain states.

## Capabilities and Constraints

- **Implemented core workflows:** Multi-account Gmail (OAuth 2.0 PKCE with in-app encrypted client configuration), IMAP/SMTP, POP3, and Microsoft Outlook/Graph; local SQLite (`v1`–`v33`) + FTS5 search; message & thread views; Go-side HTML/CSS privacy sanitization (`Strict`, `LoadOnce`, `Off`, `TrustedSender`) and per-account trusted senders; inline CID images via `gomail-attachment://` and attachment Download / Save As; encrypted local drafts, explicit Gmail cloud draft copy management, and durable Outbox; server label/folder management; global and per-account proxy/network routing; keyboard shortcuts and command palette; message-detail reading templates; signed in-app auto-updates with bsdiff deltas and GitHub Releases fallback.
- **Unified visual system:** One Clarity desktop interface (stored id `harbor`) modelled on the platform mail client—full-height sidebar, avatar message list, focus-aware selection— with `light` / `dark` / `system` color modes and independent message-detail reading templates.
- **Guarded upcoming views:** Views and settings tabs currently gated by feature availability (`Kanban`, `Contacts`, `Snoozed`, `Rules`, `AI`, `Translation`, `Cloud Sync / Backup`, plus disabled controls in `General` / `SearchFilters`) share the same Clarity visual system while strictly preserving availability guards (`isFeatureAvailable`, `isViewAvailable`, `isSettingsTabAvailable`) and truthful unavailable messaging.
- **Local-only runtime constraint:** Must not rely on external network CDNs for fonts, icons, or assets at runtime; everything ships locally in the bundle or uses OS-available typography.
- **Behavior and durability integrity:** Preserve all Go/MyGO service bindings (`src/mygo.ts`), typed platform commands, accessibility semantics, keyboard shortcuts, bilingual locale keys, and data durability/safety prompts (privacy banners, outbox unknown-outcome warnings, destructive delete confirmations).

## Roadmap

- **Local organization:** Local labels, contacts directory & vCard import/export, Kanban board, snooze scheduling, mail rules, email templates, advanced search filters, and account reordering.
- **Desktop integration:** Native desktop notifications, system tray & start-hidden mode, login autostart, default mailto handler, and in-app diagnostic log viewer.
- **Optional productivity & backup:** Encrypted file/WebDAV backup & restore, configurable translation, and opt-in AI writing/summarization tools.

## Brand Commitments

- Product name: **GoMail**.
- Application identifier & OS keyring service: `dev.bary.gomail`.
- Existing app icon asset: `src/assets/app-icon.png` / `resources/icon.png`.
- Voice: Plain, calm, honest, and non-technical in everyday surfaces; explicit and transparent when communicating privacy, offline, or uncertain remote-write states.

## Evidence on Hand

- Live React + TypeScript application source under `src/` (layout shell, sidebar, inbox/thread/reader, starred, search, compose/drafts/outbox, settings tabs, command palette, dialogs, guarded upcoming views) and Go backend under `internal/`.
- Bilingual locale catalogs in `src/locales/zh.json` and `src/locales/en.json`.
- App icon assets in `src/assets/app-icon.png` and `resources/icon.png`.
- No external marketing testimonials, benchmarks, or third-party endorsements exist in the repository; future work must not fabricate them.

## Product Principles

1. **Approachable over arcane:** Everyday reading, writing, and account switching must feel immediately clear to general desktop email users without requiring power-user knowledge.
2. **Reading comfort first:** Long-form messages, multi-message threads, and bilingual Chinese/English copy deserve generous breathing room, calm hierarchy, and effortless scannability.
3. **Honest local-first state:** Sync progress, offline status, privacy blocking, queued remote writes, and uncertain send outcomes must be clear and trustworthy without causing alarm fatigue.
4. **One coherent workspace:** Every view—from inbox and compose to settings and guarded upcoming tabs—belongs to one unified desktop application rather than a patchwork of disparate styles.

## Accessibility & Inclusion

- Full keyboard navigability (shortcut system, command palette, `focus-visible` outlines, roving tablists in settings) and semantic ARIA landmarks/roles (`role="tablist"`, `role="dialog"`, `aria-current`, `aria-expanded`, `role="alert"` / `role="status"`).
- Readable contrast across both light and dark modes in Chinese and English text.
