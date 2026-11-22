---
name: "Pebble Go"
description: "Harbor Chartroom — a calm, local-first coastal reading and correspondence workspace for desktop email."
colors:
  marine-cerulean: "#155e8a"
  marine-cerulean-hover: "#104c71"
  marine-mist: "#dfecf3"
  marine-active-border: "#b7d3e3"
  row-selected-bg: "#ebf4fa"
  row-selected-border: "#a6cadf"
  night-marine-cerulean: "#62b3e3"
  night-marine-cerulean-hover: "#7bc3ec"
  night-marine-mist: "#1b3449"
  night-marine-contrast: "#08131d"
  night-marine-active-border: "#335b7d"
  night-row-selected-bg: "#1c3349"
  night-row-selected-border: "#3d6d94"
  terracotta-signal: "#b84a24"
  terracotta-signal-bg: "#fdf4ee"
  terracotta-signal-border: "#edd0be"
  terracotta-signal-ink: "#843115"
  night-terracotta-signal: "#ee936e"
  night-terracotta-signal-bg: "#2a1d19"
  night-terracotta-signal-border: "#5a382c"
  night-terracotta-signal-ink: "#f6baa0"
  kelp-success: "#1b6e56"
  amber-warning: "#9a5710"
  buoy-danger: "#b83327"
  night-kelp-success: "#5cc49f"
  night-amber-warning: "#ebb567"
  night-buoy-danger: "#f18c82"
  harbor-mist: "#ebf1f4"
  harbor-mist-hover: "#dfe9ef"
  harbor-mist-active: "#d6e6f0"
  chalk-sea: "#f7fafb"
  reading-sheet: "#ffffff"
  surface-hover: "#edf3f6"
  surface-secondary: "#e3ecf1"
  harbor-hairline: "#d4e0e7"
  harbor-hairline-strong: "#b6c9d5"
  navy-ink: "#142538"
  slate-ink: "#435a70"
  harbor-muted-ink: "#526a80"
  scrollbar-thumb: "#bfd0db"
  scrollbar-thumb-hover: "#90abc0"
  scrim-backdrop: "rgb(14 23 33 / 44%)"
  deep-harbor-chrome: "#0e1721"
  deep-harbor-hover: "#162433"
  deep-harbor-active: "#1c3145"
  deep-harbor-list: "#131e2a"
  deep-harbor-sheet: "#172432"
  deep-harbor-surface-hover: "#1e2e3f"
  deep-harbor-surface-secondary: "#223548"
  deep-harbor-hairline: "#25384b"
  deep-harbor-hairline-strong: "#38516a"
  night-foam-ink: "#ebf2f7"
  night-slate-ink: "#a8bfd2"
  night-muted-ink: "#88a4bc"
  night-scrollbar-thumb: "#31485e"
  night-scrollbar-thumb-hover: "#476684"
  night-scrim-backdrop: "rgb(3 9 16 / 64%)"
typography:
  display:
    fontFamily: '"Avenir Next", "SF Pro Text", "PingFang SC", "Hiragino Sans GB", "Segoe UI", system-ui, sans-serif'
    fontSize: "24px"
    fontWeight: 700
    lineHeight: 1.32
    letterSpacing: "-0.02em"
  headline:
    fontFamily: '"Avenir Next", "SF Pro Text", "PingFang SC", "Hiragino Sans GB", "Segoe UI", system-ui, sans-serif'
    fontSize: "22px"
    fontWeight: 650
    lineHeight: 1.3
    letterSpacing: "-0.015em"
  title:
    fontFamily: '"Avenir Next", "SF Pro Text", "PingFang SC", "Hiragino Sans GB", "Segoe UI", system-ui, sans-serif'
    fontSize: "18px"
    fontWeight: 650
    lineHeight: 1.3
    letterSpacing: "-0.015em"
  reading:
    fontFamily: '"Avenir Next", "SF Pro Text", "PingFang SC", "Hiragino Sans GB", "Segoe UI", system-ui, sans-serif'
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.82
    letterSpacing: "0em"
  body:
    fontFamily: '"Avenir Next", "SF Pro Text", "PingFang SC", "Hiragino Sans GB", "Segoe UI", system-ui, sans-serif'
    fontSize: "13.5px"
    fontWeight: 400
    lineHeight: 1.55
    letterSpacing: "0em"
  label:
    fontFamily: '"Avenir Next", "SF Pro Text", "PingFang SC", "Hiragino Sans GB", "Segoe UI", system-ui, sans-serif'
    fontSize: "11px"
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: "0.03em"
    fontFeature: "tnum"
  mono:
    fontFamily: '"SF Mono", "Cascadia Code", Menlo, Consolas, monospace'
    fontSize: "10.5px"
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: "0em"
    fontFeature: "tnum"
  serif:
    fontFamily: '"Iowan Old Style", "Songti SC", "Noto Serif SC", Georgia, serif'
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.85
    letterSpacing: "0em"
rounded:
  sm: "6px"
  md: "8px"
  lg: "12px"
  pill: "999px"
  full: "50%"
spacing:
  2xs: "4px"
  xs: "6px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "24px"
  2xl: "32px"
  titlebar-height: "40px"
  pane-header-height: "56px"
  statusbar-height: "28px"
  sidebar-width: "244px"
  sidebar-collapsed-width: "60px"
  list-width: "376px"
  mail-row-height: "88px"
  reader-width: "740px"
  reader-gutter: "clamp(24px, 3.4vw, 44px)"
components:
  button-primary:
    backgroundColor: "{colors.marine-cerulean}"
    textColor: "{colors.reading-sheet}"
    rounded: "{rounded.md}"
    padding: "8px 14px"
    height: "38px"
  button-primary-hover:
    backgroundColor: "{colors.marine-cerulean-hover}"
    textColor: "{colors.reading-sheet}"
  button-secondary:
    backgroundColor: "{colors.reading-sheet}"
    textColor: "{colors.navy-ink}"
    rounded: "{rounded.md}"
    padding: "7px 14px"
  button-secondary-hover:
    backgroundColor: "{colors.surface-hover}"
    textColor: "{colors.navy-ink}"
  button-destructive:
    backgroundColor: "{colors.buoy-danger}"
    textColor: "{colors.reading-sheet}"
    rounded: "{rounded.md}"
    padding: "7px 16px"
  chip-toolbar:
    backgroundColor: "{colors.reading-sheet}"
    textColor: "{colors.navy-ink}"
    rounded: "{rounded.sm}"
    padding: "5px 10px"
  chip-signal:
    backgroundColor: "{colors.terracotta-signal-bg}"
    textColor: "{colors.terracotta-signal-ink}"
    rounded: "{rounded.pill}"
    padding: "2px 8px"
  input-search:
    backgroundColor: "{colors.reading-sheet}"
    textColor: "{colors.navy-ink}"
    rounded: "{rounded.md}"
    padding: "0 11px"
    height: "36px"
  nav-row:
    backgroundColor: "transparent"
    textColor: "{colors.slate-ink}"
    rounded: "{rounded.md}"
    padding: "6px 8px"
    height: "34px"
  nav-row-active:
    backgroundColor: "{colors.harbor-mist-active}"
    textColor: "{colors.navy-ink}"
  mail-row-selected:
    backgroundColor: "{colors.row-selected-bg}"
    textColor: "{colors.navy-ink}"
    padding: "13px 18px 13px 22px"
    height: "88px"
  signal-strip:
    backgroundColor: "{colors.terracotta-signal-bg}"
    textColor: "{colors.terracotta-signal-ink}"
    padding: "10px 24px"
  choice-card:
    backgroundColor: "{colors.reading-sheet}"
    textColor: "{colors.navy-ink}"
    rounded: "{rounded.md}"
    padding: "12px 14px"
  choice-card-selected:
    backgroundColor: "{colors.marine-mist}"
    textColor: "{colors.navy-ink}"
  dialog-surface:
    backgroundColor: "{colors.reading-sheet}"
    textColor: "{colors.navy-ink}"
    rounded: "{rounded.lg}"
    padding: "24px"
  statusbar-horizon:
    backgroundColor: "{colors.harbor-mist}"
    textColor: "{colors.slate-ink}"
    padding: "0 14px"
    height: "28px"
---

# Design System: Pebble Go

## Overview

**Creative North Star: "Harbor Chartroom"**

Pebble Go is designed as a luminous coastal reading room for everyday desktop correspondence. Rather than mimicking crowded browser webmail or fracturing into competing application skins, the entire desktop shell—titlebar, collapsible account rail, message ledger, long-form reading sheet, composer, settings tabs, and guarded upcoming views—shares one disciplined architectural system. Three stepped tonal planes step smoothly toward the reading surface, letting the user's eye settle effortlessly on the message at hand while preserving instant scannability across accounts, unread counts, and sync state.

Built as a local-first Go and MyGO WebView desktop client operating across macOS, Linux, and Windows, the visual language pairs unhurried clarity with honest operational feedback. Marine Cerulean marks intentional focus and primary action, while warm Terracotta is held exclusively for high-trust operational signals—blocked trackers and remote images, offline pauses, failed or queued remote writes, and uncertain send outcomes. Everything ships locally with zero network CDN dependencies, pairing OS-native humanist sans-serifs for seamless Chinese (`zh`) and English (`en`) prose across both daylit Harbor and Deep-Harbor dark modes, while leaving independent message-detail reading templates untouched.

Confirmed visual rejections shape this restraint: Pebble Go rejects generic grey-blue SaaS inbox chrome, rejects the legacy five-theme application switcher in favor of one unified Harbor system (`harbor`) with independent `light` / `dark` / `system` modes, rejects thick colored side-stripes on rows or callouts, rejects uppercase eyebrow kickers above view headings, and rejects decorative entrance staggers in favor of brisk, purposeful state feedback.

**Key Characteristics:**
- Three-step tonal depth stepping cleanly from Harbor Mist chrome (`#ebf1f4` / `#0e1721`) through Chalk Sea lists (`#f7fafb` / `#131e2a`) to a crisp Reading Sheet (`#ffffff` / `#172432`).
- Strict 56px shared horizontal baseline aligning pane headers, message action toolbars, compose bars, and secondary mail headings across the workspace.
- Active focus isolation on mail and thread rows using a washed cerulean fill and a `1px` inset hairline border rather than heavy shadows or left stripes.
- Reserved Terracotta signal beacons uniting privacy blocking, offline notices, outbox warnings, and failed remote writes into one unmistakable channel.
- Local-only OS typography calibrated for bilingual Chinese and English reading (`15px` / `1.82` leading inside a `740px` reading sheet) with `tabular-nums` alignment across every ledger count and timestamp.
- Independent 9-template message-detail reading architecture (`app` default plus 8 adaptive and fixed-palette skins) decoupled from the unified application shell.

## Colors

The Harbor Chartroom palette pairs cool coastal blue-slate neutrals with restrained Marine Cerulean focus and strictly rationed Terracotta operational signals across both light and dark modes.

### Primary
- **Marine Cerulean** (`#155e8a`, hover `#104c71`) / **Night Marine Cerulean** (`#62b3e3`, hover `#7bc3ec`, dark button ink `#08131d`): Primary Compose CTA, primary confirmation and save buttons, active navigation icons, unread leading pips, unread timestamps, keyboard `focus-visible` outlines, input carets, and dark-sheet email links (`#155e8a` on light mail sheets).
- **Marine Mist** (`#dfecf3` / dark `#1b3449`): Active settings tab fill, selected choice-card surface, command palette selection highlight, statusbar pressed toggle fill, toast icon badge fill, sender avatar ground, and text `::selection` highlight.
- **Marine Active Border** (`#b7d3e3` / dark `#335b7d`): Crisp `1px` perimeter outlining active sidebar rows, active account rows, and selected settings navigation tabs.
- **Selected Row Wash & Border** (`#ebf4fa` & `#a6cadf` / dark `#1c3349` & `#3d6d94`): Focus isolation fill and `1px` inset border marking the active mail, thread, or search result row.

### Secondary
- **Terracotta Signal** (`#b84a24` / dark `#ee936e`): The `7px` solid privacy beacon pip, privacy shield icon, offline/error statusbar indicator dot, and error toast icon color.
- **Terracotta Signal Wash** (`#fdf4ee` / dark `#2a1d19`): Full-width surface tint for the Harbor-Channel privacy strip, offline banner, error toasts, signal callouts, and uncertain/failed outbox rows.
- **Terracotta Signal Border** (`#edd0be` / dark `#5a382c`): Hairline framing for signal banners, inline action buttons, signal beacons, and error toasts.
- **Terracotta Signal Ink** (`#843115` / dark `#f6baa0`): High-contrast text color for every message, status readout, and button label inside a Terracotta signal surface.

### Tertiary
- **Kelp Success** (`#1b6e56` / dark `#5cc49f`): Calm steady-state indicator dot on the bottom status bar when synced and ready.
- **Amber Warning** (`#9a5710` / dark `#ebb567`): Non-blocking partial-completion notices such as incomplete bilingual translation segments.
- **Buoy Danger** (`#b83327` / dark `#f18c82`): Destructive confirmation buttons, close-window hover fill, destructive context-menu items, and inline form/load error copy.

### Neutral
- **Harbor Mist Chrome** (`#ebf1f4`, hover `#dfe9ef`, active `#d6e6f0`) / **Deep Harbor Chrome** (`#0e1721`, hover `#162433`, active `#1c3145`): Outer frame plane for the 40px titlebar, collapsible sidebar navigation rail, and 28px bottom status horizon.
- **Chalk Sea List** (`#f7fafb`) / **Deep Harbor List** (`#131e2a`): Mid-luminance plane for the message and thread list panel, secondary mail lists (Starred, Search, Snoozed), Settings left navigation column, Outbox drawer, attachment cards, and fieldset containers.
- **Reading Sheet** (`#ffffff`) / **Deep Harbor Sheet** (`#172432`): Highest-clarity focal plane for the message reader, thread view, compose workspace, settings content pane, input fields, floating menus, and modal dialogs. Also serves as light-mode button ink on Marine Cerulean and Buoy Danger fills.
- **Interactive Surface Neutrals** (`#edf3f6` hover, `#e3ecf1` secondary / dark `#1e2e3f` hover, `#223548` secondary): Row hover states, secondary button hovers, count pill grounds, keyboard shortcut badge surfaces, and loading skeleton shimmers.
- **Harbor Hairlines** (`#d4e0e7` standard, `#b6c9d5` strong / dark `#25384b` standard, `#38516a` strong): Structural `1px` pane dividers, row rules (`72%` opacity blend), input borders, and stronger boundaries on secondary buttons, expanded thread bubbles, and floating overlays.
- **Ink Ramp** (`#142538` primary, `#435a70` secondary, `#526a80` tertiary / dark `#ebf2f7` primary, `#a8bfd2` secondary, `#88a4bc` tertiary): Tinted navy-slate ink (never harsh pure black) scaling smoothly from headings and unread subjects down to secondary metadata, snippets, and timestamps.
- **Scrollbars & Scrim Backdrop** (`#bfd0db` / hover `#90abc0`, scrim `rgb(14 23 33 / 44%)` / dark `#31485e` / hover `#476684`, scrim `rgb(3 9 16 / 64%)`): Subtle themed scrollbar thumbs and modal overlay backdrops. When a user-selected local background wallpaper is active, shell chrome panes blend with `color-mix` translucency (`68%`–`92%`) and `16px`–`18px` backdrop blur while preserving ink legibility.

### Named Rules
**The Three-Plane Luminance Rule.** Chrome (`#ebf1f4` / `#0e1721`), triage lists (`#f7fafb` / `#131e2a`), and the reading sheet (`#ffffff` / `#172432`) step progressively brighter in both light and dark modes so reading always owns visual focus without decorative borders.

**The Terracotta Signal Doctrine.** Warm Terracotta (`#b84a24` / `#ee936e` on `#fdf4ee` / `#2a1d19`) is reserved exclusively for privacy protection, offline status, queued/failed remote writes, and uncertain send outcomes; never spend it on promotional accents, unread badges, or routine navigation.

**The Marine Focus Scarcity Rule.** Marine Cerulean (`#155e8a` / `#62b3e3`) marks active selection, unread pips, primary CTAs, and keyboard focus rings only—never paint entire chrome regions or passive containers in the primary accent.

## Typography

**Display Font:** `"Avenir Next", "SF Pro Text", "PingFang SC", "Hiragino Sans GB", "Segoe UI", system-ui, sans-serif` (local OS stack, zero external CDN)
**Body Font:** `"Avenir Next", "SF Pro Text", "PingFang SC", "Hiragino Sans GB", "Segoe UI", system-ui, sans-serif`
**Label/Mono Font:** `"SF Mono", "Cascadia Code", Menlo, Consolas, monospace` (for keyboard shortcut caps, HTML/Markdown source code mode, and inline `<code>` snippets)
**Serif Companion (Templates):** `"Iowan Old Style", "Songti SC", "Noto Serif SC", Georgia, serif` (reserved for serif message-detail templates such as Letter and Claude)

**Character:** A single calm, local-first humanist sans-serif spine unifies every workspace pane without jarring display-font theatrics. Pairing macOS/Windows system sans faces directly with `PingFang SC` and `Hiragino Sans GB` keeps English and Chinese glyph metrics balanced at desktop reading sizes while honoring Pebble Go's strict offline, zero-CDN runtime commitment.

### Hierarchy
- **Display** (`700`, `24px`, `1.32` line-height, `-0.02em` tracking, `text-wrap: balance`, clamping to `22px` below `520px`): Message subject headings inside the native Harbor reading sheet.
- **Headline** (`650`, `22px` in Settings / `20px` in Thread Reader, `1.3`–`1.32` line-height, `-0.015em` tracking): Top-level Settings panel headings and thread conversation subjects.
- **Title** (`650`, `18px`, `1.3` line-height, `-0.015em` tracking): Pane header titles across Inbox list, Starred, Compose, and empty-reader guidance headings.
- **Reading Body** (`400`, `15px`, `1.82` line-height): Long-form email prose and rich-text/Markdown compose editor body inside a `740px` (~65–75ch) reading column.
- **UI Body** (`400`–`600`, `13.5px` base / `13px` controls & rows, `1.55` line-height): Everyday navigation labels, sender names (`13.5px` `500` read / `650` unread), subjects (`13px` `450` read / `600` unread), settings controls, and dialog body copy (`13px` / `1.6`).
- **Label** (`600`, `11px`–`12px`, `1.3`–`1.45` line-height, `0.03em` tracking, sentence/natural case): Sidebar group labels (`11px`, `text-transform: none`), unread pills (`11px`), row dates (`11px`), row snippets (`12px`), statusbar horizon copy (`11.5px`), and privacy/settings descriptions (`12.5px`).
- **Mono** (`500`, `10.5px`–`11px`, `1.4` line-height, `tabular-nums`): Shortcut keycaps (`⌘N`, `/`), command palette key hints, and HTML source editing.

### Named Rules
**The Bilingual Legibility Rule.** Never apply `text-transform: uppercase` to navigation section headings or bilingual UI labels; preserve natural casing, maintain `1.82` reading line-height for dense Chinese and English paragraphs, and truncate CJK sender monograms to a single glyph (vs. up to two Latin initials).

**The Tabular Ledger Rule.** Apply `font-variant-numeric: tabular-nums` to every unread count pill, message timestamp, thread date, statusbar readout, and shortcut cap so numbers stay vertically locked during live sync updates.

## Layout

Pebble Go arranges the desktop window into a fixed vertical shell wrapping a calm three-column horizontal workspace:
- **Top Titlebar (`40px` height):** Full-width Harbor Mist drag bar carrying the `20px` Pebble Go icon, brand name (`12.5px` `650`), hairline-separated active view breadcrumb (`12px`), and platform window controls (`84px` leading inset on macOS for traffic lights; `44px`-wide minimize/maximize/close buttons on Windows/Linux).
- **Collapsible Navigation Rail (`244px` expanded / `60px` collapsed):** Pinned Compose primary CTA (`12px 12px 10px` wrap), Search entry field, scrollable mailbox & nested-folder column (the only scrolling section in the rail, with `1px` nesting hairline at `17px` left), hairline divider, and pinned bottom tools (Contacts, Kanban, Settings, Collapse toggle).
- **Message & Thread List Column (`clamp(280px, 36%, 376px)`):** Fixed `56px` pane header flush with the reader toolbar, embedded search & filter chip toolbar (`10px 14px`), and virtualized `88px` rows (`13px 18px 13px 22px` padding).
- **Reading & Workspace Pane (fluid `1fr`):** Opens with a `56px` action toolbar aligned horizontally with the list header, optional full-width Harbor-Channel Signal Strip directly underneath, and a single scrolling document pane (`32px` top, `clamp(24px, 3.4vw, 44px)` horizontal gutter, `56px` bottom) centering a `740px` reading sheet.
- **Settings Workspace:** Splits into a `188px` Chalk Sea roving vertical tablist (`158px` at `<=1100px`) and a pure Reading Sheet panel holding a `780px` standard column (`1020px` wide column for Appearance and Outbox/Remote Writes).
- **Bottom Status Horizon (`28px` height):** Full-width Harbor Mist footer with `14px` horizontal inset and `12px` item gap reporting sync readiness, realtime transport state, manual sync trigger, queued/failed remote-write beacons, and background-run toggle.

**Spacing Rhythm & Scrollbars:** Spacing steps follow a compact desktop scale (`4px`, `6px`, `8px`, `12px`, `16px`, `24px`, `32px`). Scrollable regions declare `overscroll-behavior: contain` and `scrollbar-gutter: stable` above `760px` so loading message bodies or folder lists never shifts horizontal alignment.

**Responsive Breakpoints:**
- `<=1100px`: Settings navigation rail trims from `188px` to `158px`.
- `<=900px`: Reader horizontal scroll padding tightens to `22px`.
- `<=760px`: Stable scrollbar gutters release; expanded sidebar caps at `42vw`; mail and secondary lists span `100%` width and hide the empty reader; opening a message detail hides both the list and sidebar so the reader owns `100%` width; Settings switches from a left rail to a single-row horizontally scrollable `48px` tab bar (`aria-orientation="horizontal"`); Compose header actions wrap onto a full-width equal-flex row.
- `<=560px`: While Settings is active, the sidebar automatically compacts to a `52px` icon-only rail so form controls retain usable horizontal room without disappearing navigation.
- `<=520px`: Reader toolbar compacts (`6px 10px`), reader padding narrows to `20px 16px 36px`, display subject caps at `22px`, and the sender identity grid stacks vertically (`from` -> `recipients` -> `date` -> `contacts`).

### Named Rules
**The 56px Shared Horizon Rule.** Every primary pane header—Inbox list header, native Reader action toolbar, Thread reader header, Compose header, Starred header, and Search toolbar—holds a `56px` minimum height with a `1px` bottom hairline so adjacent panes share one continuous horizontal datum line.

**The Single-Slot Ledger Rule.** Reserve a fixed `30px` trailing slot (`24px` below `760px`) for unread counts on every sidebar row and align nested folder labels to the parent account's trailing edge so counts form one vertical column and never shift labels when mail arrives.

## Elevation & Depth

Harbor Chartroom is **tonally layered and flat at rest**. Structural hierarchy between chrome, lists, reading surfaces, and fieldsets comes from stepped background luminance and `1px` harbor hairlines—never heavy drop shadows or floating cards around everyday text. Shadows appear selectively to lift primary triggers, active disclosures, and transient overlays above the workspace plane.

### Shadow Vocabulary
- **Subtle Lift (`--ui-shadow-sm`)** — Light: `box-shadow: 0 1px 2px rgb(20 37 56 / 6%), 0 2px 6px rgb(20 37 56 / 4%)` / Dark: `box-shadow: 0 2px 6px rgb(4 10 18 / 28%)`: Resting Compose primary CTA, expanded thread message bubbles, floating row-hover quick-action tray, Compose editor frame, and unified Harbor theme preview card.
- **Interactive & Floating Layer (`--ui-shadow-md`)** — Light: `box-shadow: 0 6px 20px rgb(20 37 56 / 10%), 0 2px 6px rgb(20 37 56 / 5%)` / Dark: `box-shadow: 0 8px 24px rgb(4 10 18 / 40%)`: Compose primary CTA hover state, toast notifications, and portalled folder context menus.
- **Modal Surface (`--ui-shadow-lg`)** — Light: `box-shadow: 0 18px 44px rgb(12 24 38 / 22%), 0 4px 14px rgb(12 24 38 / 10%)` / Dark: `box-shadow: 0 22px 52px rgb(2 8 15 / 56%)`: Command Palette (`z-index: 1000`) and Confirm Dialog (`z-index: 1100`) floating over the translucent scrim backdrop.
- **Focus Isolation Ring** — `box-shadow: inset 0 0 0 1px var(--color-row-selected-border)` (mail/search selected rows) and `box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--color-accent) 26%, transparent)` (selected Settings choice cards): Crisply isolates active selection without shifting adjacent layout geometry.
- **Input Focus Halo** — `box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-accent) 14%, transparent)` paired with `border-color: var(--color-accent)`: Marks focused search and settings text inputs alongside global `2px solid var(--color-accent)` (`2px` offset) keyboard `:focus-visible` outlines.

**Motion & Reduced-Motion Grammar:** Interactive state changes use a crisp exponential ease-out curve (`cubic-bezier(0.22, 1, 0.36, 1)`) at `150ms` (`--ui-motion-fast`) for hover/focus/selection fills and `190ms` (`--ui-motion-base`) for sidebar width collapse and toast entrance (`toast-in`). Switching color modes sets `html[data-theme-switching]` to clamp motion tokens to `0ms` so surfaces swap cleanly without cross-fade smearing. Under `@media (prefers-reduced-motion: reduce)`, both a global WCAG 2.3.3 guard (`0.01ms` duration cap) and per-stylesheet rules disable transitions and animations across shell, list, reader, picker, and workspace controls.

### Named Rules
**The Tonal-First Flatness Rule.** Keep navigation panes, list rows, reading sheets, and form fieldsets flat at rest; spend elevation shadows exclusively on primary triggers, expanded thread disclosures, floating menus, toasts, and modal overlays.

**The Crisp State-Only Motion Rule.** Limit motion to direct 150ms–190ms state responses (`cubic-bezier(0.22, 1, 0.36, 1)`); suppress transitions during theme switches and zero them completely under `prefers-reduced-motion: reduce`.

## Shapes

Harbor Chartroom uses an approachable, disciplined three-step corner system paired with crisp `1px` borders and zero decorative stripes:
- **Compact Controls (`6px` radius):** Filter chips, privacy strip action buttons, context menu rows, statusbar icon buttons, and subtle inline alerts (`5px` for micro keyboard shortcut caps).
- **Primary Interactive Elements (`8px` radius):** Primary/secondary/destructive buttons, sidebar navigation and account rows, search inputs, settings form inputs and choice cards, inline reader privacy banners, attachment cards, floating row action trays, toasts, and context menus.
- **Structural Containers & Overlays (`12px` radius):** Settings fieldsets, outbox panels, thread message bubbles, Command Palette, Confirm Dialog, empty-state reader icon frame, and the Harbor appearance preview card.
- **Full Pills (`999px` radius):** Unread count pills, list count badges, outbox status pills, bottom statusbar beacons, and thin rounded scrollbar thumbs (`10px` shell / `8px` settings).
- **Circular (`50%` radius):** Sender and account avatars (`26px` sidebar, `38px` native reader), `6px` unread and statusbar indicator dots, `7px` privacy beacon dots, and `24px` toast icon badges.

All interactive borders remain `1px solid` (`--color-border` at rest, `--color-border-strong` on hover or elevated frames, `--color-active-border` / `--color-accent` when selected). No component uses thick (`3px`–`4px`) colored left border bars.

## Components

Every component feels calm, tactile, and immediately legible—signaling state through clean tonal shifts and hairline crispness rather than visual noise.

### Buttons
- **Shape:** Gently rounded rectangle (`8px` radius).
- **Primary:** Marine Cerulean fill (`#155e8a` / dark `#62b3e3`), contrasting ink (`#ffffff` / dark `#08131d`), `600` weight, `13px` type, `8px 14px` padding (`min-height: 38px` with `--ui-shadow-sm` and trailing mono shortcut cap for Sidebar Compose CTA).
- **Hover / Focus / Disabled:** Transitions in `150ms` to deep/bright hover tone (`#104c71` / dark `#7bc3ec`; Compose CTA lifts to `--ui-shadow-md`). Keyboard focus draws `2px solid var(--color-accent)` (`-3px` inset on window/compose chrome, `2px` outset elsewhere). Disabled dims to `0.5`–`0.6` opacity with shadow removed and `cursor: not-allowed` / `default`.
- **Secondary:** Reading Sheet fill (`#ffffff` / dark `#172432`), primary ink, `1px solid` border (`--color-border` or `--color-border-strong`), `7px 14px`–`16px` padding, hovering to `--color-bg-hover`.
- **Destructive:** Buoy Danger fill (`#b83327` / dark `#f18c82`), `#ffffff` ink, `600` weight, `7px 16px` padding—reserved for confirmed irreversible actions.
- **Icon & Window Controls:** Transparent at rest, `30px × 30px` (`8px` radius) in list toolbars, `22px × 22px` (`6px` radius) in the status bar (`aria-pressed="true"` fills with Marine Mist + Marine Cerulean), and `44px`-wide full-height titlebar controls (Close hovers to Buoy Danger with white glyph).

### Chips
- **Toolbar Chips:** `6px` radius, `5px 10px` padding, `12px` `500` weight, Reading Sheet background with `1px solid var(--color-border)`, shifting to `--color-bg-hover` and `--color-border-strong` on hover (`0.45` opacity when disabled).
- **Count Pills:** Full pill (`999px`), `11px` `600` `tabular-nums`; sidebar counts use `12%` Marine Cerulean tint + Marine Cerulean ink (softening to `18%` Marine Cerulean blended over sheet bg when row is selected); pane headers and outbox headers use `--color-bg-secondary` + secondary ink.
- **Status & Signal Beacons:** Full pill (`999px`), `1px 8px` (statusbar) or `2px 8px` (outbox) padding, `11px` `500`–`600`; default uses neutral sheet/border or Marine Mist; signal variants switch to Terracotta wash, border, and high-contrast Terracotta ink.

### Cards / Containers
- **Settings Choice Cards:** `8px` radius, `12px 14px` padding, Reading Sheet fill, `1px solid var(--color-border)`, flexing `1 1 180px`; hovering shifts to Chalk Sea + strong border; selected (`aria-pressed="true"`) shifts to Marine Mist fill, Marine Cerulean border, and subtle `1px` inset accent ring.
- **Thread Message Bubbles:** `12px` radius, `max-width: 740px`, `1px solid var(--color-border)`, `13px 18px` toggle header and `20px 22px` expanded body; expanding deepens border to `--color-border-strong`, tinting the header strip Chalk Sea and lifting with `--ui-shadow-sm`.
- **Fieldsets & Callouts:** Fieldsets use `12px` radius, `16px 18px` padding, Chalk Sea ground, and `1px` hairline border. Callouts use `8px` radius, `12px 16px` padding (Terracotta variant switches all three channels to signal tokens).
- **Overlays (Command Palette, Confirm Dialog, Toasts, Context Menus):** Command Palette (`max-width: 520px`, `12px` radius, `--ui-shadow-lg`) and Confirm Dialog (`max-width: 400px`, `12px` radius, `24px` padding, `--ui-shadow-lg`) sit centered or top-weighted (`16vh`) over `--color-backdrop`. Toasts (`280px`–`420px`, `8px` radius, `--ui-shadow-md`, bottom-right `44px`/`20px`) carry a `24px` circular icon badge and switch cleanly to Terracotta tokens for errors. Context menus (`min-width: 214px`, `8px` radius, `5px` inset, `--ui-shadow-md`) house `6px`-radius items with Buoy Danger styling on destructive options.

### Inputs / Fields
- **Search Input Shell:** `36px` height, `0 11px` padding, `8px` radius, Reading Sheet background, `1px solid var(--color-border)`, `12.5px` input text, Marine Cerulean caret, focusing with a Marine Cerulean border and `3px` `14%` accent focus halo.
- **Settings Form Controls:** Text, email, password, number, URL, select, and textarea fields adopt `8px` radius, Reading Sheet background, `1px` harbor hairline border, native `color-scheme` sync (`light` / `dark`), accent-colored checkboxes/radios, and the same `3px` Marine Cerulean focus halo.
- **Compose Editor Surface:** `8px` radius, `1px` hairline border, Reading Sheet fill with `--ui-shadow-sm`, `15px` / `1.82` reading typography (`--font-mono` when toggled to HTML/source code mode), paired with `6px`-radius formatting toolbar buttons.

### Navigation
- **Collapsible Sidebar Rail:** `244px` Harbor Mist column (`60px` collapsed icon rail) using uniform `34px`-min-height, `8px`-radius rows (`13px` `500` secondary ink; `12.5px` when nested under a mailbox with a `1px` vertical guide line at `17px`). Hover tints to `--color-sidebar-hover` with primary ink; active (`data-selected="true"` / `aria-current="page"`) sets `--color-sidebar-active`, `1px solid var(--color-active-border)`, `600` weight, and Marine Cerulean icon color. When collapsed, unread state condenses to a `6px` Marine Cerulean dot on the icon corner.
- **Settings Roving Tablist:** `188px` Chalk Sea rail (`role="tablist"`, arrow/Home/End keyboard roving) with `8px 12px` `8px`-radius tabs; active tab takes Marine Mist background, active border, and Marine Cerulean `600` ink. Guarded/unavailable tabs stay truthfully visible at `0.48` opacity (`disabled`, `cursor: not-allowed`) paired with a calm `.settings-availability-note` banner (`12.5px` on Chalk Sea).

### Mail Rows & Focus Isolation
- **Anatomy:** `88px`-tall Chalk Sea rows (`13px 18px 13px 22px` padding) separated by soft `72%` harbor hairlines (`border-radius: 0`).
- **Unread & Selection States:** Unread rows mark themselves with a `6px` circular Marine Cerulean leading pip (`top: 20px; left: 8px`), elevated sender (`650`) and subject (`600`) weights in primary ink, and a Marine Cerulean `600` tabular timestamp—never altering row background so selection remains unmistakable. Selected rows (`aria-selected="true"`) isolate focus via `#ebf4fa` / `#1c3349` fill plus `inset 0 0 0 1px` selection border (`z-index: 1`). Hovering non-selected rows lifts to `--color-bg-hover` and reveals a right-anchored `8px`-radius floating quick-action tray (`--ui-shadow-sm`).

### Harbor-Channel Signal Strip
- **Purpose:** Signature full-width operational strip uniting privacy blocking (`PrivacyBanner`) directly above the reading canvas and offline notices (`OfflineBanner`) directly beneath the titlebar.
- **Treatment:** Terracotta Signal Wash (`#fdf4ee` / `#2a1d19`), `1px` bottom Terracotta hairline border (`10px 24px` full-bleed; inset `8px`-radius card with `10px 14px` padding when inside an expanded thread bubble), led by a `7px` solid Terracotta beacon pip and `14px` Shield icon. Inline action buttons (`Load images`, `Trust images`, `Trust sender`) use `6px` radius, `4px 10px` padding, Reading Sheet fill, and Terracotta border/ink, tinting `10%` Terracotta on hover. Errors announce via `role="alert"`.

### Bottom Horizon Status Bar
- **Purpose:** `28px` Harbor Mist operational horizon anchoring the bottom edge of the desktop window.
- **Treatment:** `11.5px` `tabular-nums` copy led by a `6px` semantic dot (`--color-success` when Ready, `--color-accent` when Syncing, `--color-signal` on Sync Error or Offline), polite/assertive ARIA live regions (`role="status"` / `role="alert"`), `22px` manual sync & background-run icon buttons, and compact pill beacons surfacing queued, retrying, or failed remote writes.

### Message Detail Reading Templates
- **Independent Architecture:** While Pebble Go's application shell is permanently unified under Harbor Chartroom (`harbor`), `MessageDetail` preserves an independent 9-template reading system (`src/lib/messageThemes.ts`) switchable from Appearance Settings or the reader toolbar's palette trigger.
- **Template Families:**
  - *Adaptive Templates* (`app` [default "Follow interface"], `card`, `mailbox`): Consume Harbor CSS custom properties directly so light/dark switching is automatic. Default `app` mode renders as one seamless scrolling document (`740px` sheet, `24px` display subject, `38px` circular sender monogram, `15px` / `1.82` body) without nested cards.
  - *Fixed-Palette Templates* (`letter`, `console`, `claude`, `wechat`, `telegram`, `imessage`): Own distinct typography, surface framing, and layout structures (centered serif correspondence, two-column dark console, messaging-inspired layouts) and resolve dedicated night palettes in JS via `resolveMessageTheme`.
- **Sanitized HTML Isolation:** Templates publish `--msg-*` custom properties and `data-msg-*` structural switches to CSS and inject scoped typography into `ShadowDomEmail`. In dark mode, adaptive templates park light-authored HTML mail on a readable white sheet (`#ffffff` / `#202124`) while overriding links in `app` mode to light Marine Cerulean (`#155e8a`) so dark-mode pale blue never loses contrast on white email paper.

## Do's and Don'ts

### Do:
- **Do** step surfaces cleanly across the three luminance planes—Harbor Mist chrome (`#ebf1f4` / `#0e1721`), Chalk Sea lists (`#f7fafb` / `#131e2a`), and Reading Sheet (`#ffffff` / `#172432`)—for every view including Settings and guarded views.
- **Do** align every primary pane header, reader toolbar, compose bar, and secondary mail heading to the `56px` shared horizontal baseline (`--ui-pane-header-height`).
- **Do** reserve Terracotta signal tokens (`--color-signal*`) strictly for privacy blocking, offline state, queued/failed remote writes, and uncertain send outcomes.
- **Do** set `font-variant-numeric: tabular-nums` on every count badge, date, timestamp, shortcut badge, and status bar readout.
- **Do** use the local OS font stack (`--font-ui`, `--font-mono`, `--font-serif`) with `1.82` reading line-height inside a `740px` measure so bilingual Chinese and English mail renders comfortably offline with zero CDN requests.
- **Do** preserve keyboard `:focus-visible` outlines (`2px solid var(--color-accent)`), roving ARIA tablists, live status regions, `html[data-theme-switching]` zero-flash switching, and `prefers-reduced-motion: reduce` overrides on all interactive controls.
- **Do** keep message-detail reading templates (`--msg-*` / `data-msg-*`) decoupled from application chrome tokens and preserve the dark-mode light-sheet link contrast guard (`#155e8a`).
- **Do** render guarded/upcoming features (`Kanban`, `Contacts`, `Snoozed`, `Rules`, `AI`, `Translation`, `Cloud Sync`) inside the unified Harbor surface language with honest disabled states and clear unavailability notes.

### Don't:
- **Don't** add thick (`3px`–`4px`) colored side-stripes to mail rows, privacy banners, callouts, or active navigation items; use `1px` hairlines, leading `6px` pips, or inset focus-isolation borders instead.
- **Don't** place uppercase eyebrow kickers above pane titles, settings headers, or dialog headings, and never apply `text-transform: uppercase` to bilingual Chinese/English navigation section labels.
- **Don't** reintroduce multiple competing application-shell themes; keep `harbor` as the single application visual system with independent `light` / `dark` / `system` color modes.
- **Don't** load external web fonts, icon CDNs, or third-party stylesheets at runtime.
- **Don't** paint unread mail rows with a competing background fill that fights selected-row focus isolation, or reuse Terracotta signal hues for decorative accents.
- **Don't** add decorative staggered entrance animations to workspace lists, tabs, or dialogs; keep motion strictly to `150ms`–`190ms` state transitions.
- **Don't** hardcode raw `#ffffff` button text against `var(--color-accent)` without using `var(--color-accent-contrast)` (`#08131d` in dark mode), or dark-mode primary buttons lose text contrast.
