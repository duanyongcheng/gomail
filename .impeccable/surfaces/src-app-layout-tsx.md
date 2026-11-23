---
version: 1
slug: "src-app-layout-tsx"
primary_target: "src/app/Layout.tsx"
related_targets: ["src/components/Sidebar.tsx","src/features/inbox/InboxView.tsx","src/features/settings/SettingsView.tsx","src/features/compose/ComposeView.tsx","src/features/search/SearchView.tsx","src/features/starred/StarredView.tsx"]
---

# Surface Brief — GoMail All-Pages Redesign

- **Scope:** Full desktop application redesign across shell (titlebar, sidebar, status bar, command palette, dialogs), mail workspace (inbox, message list, thread view, reader, starred, search), compose workspace (composer, drafts, outbox), settings (all 12 tabs including appearance convergence to one unified design system while keeping color mode and independent message templates), and guarded upcoming views (kanban, contacts, snoozed).
- **Visitor mode:** Operate (desktop mail client for general everyday office and personal users, bilingual Chinese/English, light and dark modes).
- **Audience & job:** General desktop email users triaging multi-account mail, reading long bilingual messages comfortably, composing replies/drafts, and monitoring offline/privacy/remote-write states without technical jargon or visual clutter.
- **Constraints:** Preserve all Go/MyGO service bindings, IPC commands, availability guards, accessibility landmarks/focus-visible/keyboard shortcuts, local-only zero-CDN typography, light/dark/system modes, and independent message reading templates. Converge legacy 5-theme switcher into one unified Harbor Chartroom system.

## Direction contract

THESIS: Every GoMail screen operates as one calm, luminous Harbor Chartroom—an unhurried coastal reading workspace that refuses both the generic grey-blue SaaS inbox and the old patchwork of five competing themes, tying navigation, reading, writing, and settings to one shared architectural baseline.

OWN-WORLD: Harbor Chartroom light palette pairs harbor-mist chrome (`#EBF1F4` titlebar, sidebar, status bar) and chalk-sea list surface (`#F7FAFB`) against a pure white reading sheet (`#FFFFFF`), deep navy-slate ink (`#142538`, tinted secondary `#4C6378`), crisp harbor hairlines (`#D3E0E7`), marine-cerulean primary/selection accent (`#155E8A` / soft tint `#DFECF3`), and warm terracotta-amber (`#C2522B` on `#FDF4EE`) reserved strictly for signal beacons (privacy tracker blocking, offline status, queued/uncertain remote writes). Deep-harbor dark mode (`#0E1721` shell, `#131E2A` list, `#172432` reader, `#EBF2F7` ink, `#58A8D8` marine accent, `#EE936E` amber signal). Single clean humanist sans UI stack with `tabular-nums` for dates/counts, unified `8px`/`10px` radii, no thick colored side-stripes, no eyebrow kickers above headings, and themed selection/caret/scrollbars/focus rings.

STORY: Opening GoMail immediately conveys a mature, trustworthy local-first desktop app: the user scans accounts and unread mail in one glance, reads long Chinese/English messages in an unobstructed 65–75ch reading sheet, trusts explicit amber signal beacons for privacy and sync/outbox states, and moves between Inbox, Starred, Search, Compose, and Settings without ever feeling like they crossed into a different application.

FIRST VIEWPORT: 40px harbor-mist titlebar with brand identity and concise active view context; 244px collapsible harbor-mist sidebar led by the primary marine-cerulean Compose CTA (`⌘N`), Search, pinned cross-account views (Starred/Snoozed), and grouped mailboxes with canonical single-slot unread counts; 376px chalk-sea message list with 56px aligned pane header, embedded search/filter controls, and calm rows featuring a subtle leading unread pip and crisp active focus isolation; pure white reading pane with matching 56px action bar, signature Harbor-Channel Signal Strip uniting privacy/tracker actions directly above the spacious 65–75ch reading canvas, and a 28px harbor-mist bottom horizon bar reporting online/offline cache, outbox, and pending-write status. Signature interaction and motion grammar: crisp 150–200ms exponential ease-out state transitions (`cubic-bezier(0.22, 1, 0.36, 1)`) on sidebar collapse, selection focus isolation, thread bubble disclosure, and status-beacon updates—never decorative entrance choreography.

FORM: Maritime Chartroom & Coastal Signal (Harbor Chartroom), candidate 7 of 7, raised with active focus isolation (from `pop-culture-shelf-streaming-title-card-wall`), strict shared horizontal header baseline across all views (from `crouwel-grid-specimen`), and zero redundant labeling (from `brand-identity-canon-saville-catalog-sleeve`), `seed:e87dd03e4144b171`.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
