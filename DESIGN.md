---
name: "GoMail"
description: "Clarity — a native three-pane desktop mail workspace: full-height source-list sidebar, comfortable avatar message list, focus-aware selection, and a quiet white reading pane."
colors:
  system-blue: "#0064e0"
  system-blue-hover: "#0057c4"
  system-blue-soft: "#e2edfc"
  selection-fill: "#0064e0"
  selection-inactive: "#e3e3e8"
  unread-dot: "#0a6ef0"
  flag-amber: "#f5a300"
  night-system-blue: "#4a9eff"
  night-system-blue-fill: "#1c6fe3"
  night-selection-fill: "#0f5bd0"
  night-selection-inactive: "#3a3a3d"
  sidebar: "#efeef2"
  sidebar-hover: "#e4e3e9"
  sidebar-active: "#dad9e0"
  content: "#ffffff"
  content-hover: "#f3f3f6"
  content-secondary: "#f0f0f3"
  hairline: "#e2e1e6"
  hairline-strong: "#c9c8cf"
  separator: "#e8e7ec"
  ink: "#1d1d1f"
  ink-secondary: "#5c5c61"
  ink-tertiary: "#737378"
  night-sidebar: "#28282b"
  night-sidebar-hover: "#333336"
  night-sidebar-active: "#3e3e42"
  night-content: "#1e1e20"
  night-content-hover: "#2a2a2d"
  night-content-secondary: "#2c2c2f"
  night-hairline: "#38383b"
  night-separator: "#303033"
  night-ink: "#f5f5f7"
  night-ink-secondary: "#ababb0"
  night-ink-tertiary: "#929297"
  signal: "#c27400"
  signal-bg: "#fff6df"
  signal-border: "#f0dca8"
  signal-ink: "#6f4500"
  night-signal: "#ffb340"
  night-signal-bg: "#2f2615"
  night-signal-ink: "#ffd48a"
  success: "#1a7f37"
  warning: "#9a5800"
  error: "#c4161c"
  danger: "#d70015"
typography:
  pane-title:
    fontFamily: '-apple-system, BlinkMacSystemFont, "SF Pro Display", "PingFang SC", "Hiragino Sans GB", "Segoe UI Variable Display", "Segoe UI", "Microsoft YaHei UI", system-ui, sans-serif'
    fontSize: "15px"
    fontWeight: 700
    lineHeight: 1.27
    letterSpacing: "-0.01em"
  settings-title:
    fontFamily: "{typography.pane-title.fontFamily}"
    fontSize: "20px"
    fontWeight: 700
    lineHeight: 1.3
    letterSpacing: "-0.01em"
  reader-subject:
    fontFamily: "{typography.pane-title.fontFamily}"
    fontSize: "17px"
    fontWeight: 600
    lineHeight: 1.35
  reader-sender:
    fontFamily: '-apple-system, BlinkMacSystemFont, "SF Pro Text", "PingFang SC", "Hiragino Sans GB", "Segoe UI Variable Text", "Segoe UI", "Microsoft YaHei UI", system-ui, sans-serif'
    fontSize: "14px"
    fontWeight: 700
    lineHeight: 1.4
  reading:
    fontFamily: "{typography.reader-sender.fontFamily}"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.7
  body:
    fontFamily: "{typography.reader-sender.fontFamily}"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.45
  row-sender:
    fontFamily: "{typography.reader-sender.fontFamily}"
    fontSize: "13px"
    fontWeight: 600
    lineHeight: 1.38
  row-preview:
    fontFamily: "{typography.reader-sender.fontFamily}"
    fontSize: "12px"
    fontWeight: 400
    lineHeight: 1.375
  label:
    fontFamily: "{typography.reader-sender.fontFamily}"
    fontSize: "11px"
    fontWeight: 600
    lineHeight: 1.3
    fontFeature: "tnum"
  mono:
    fontFamily: 'ui-monospace, "SF Mono", "Cascadia Code", Menlo, Consolas, monospace'
    fontSize: "12px"
    fontWeight: 400
    lineHeight: 1.5
rounded:
  sm: "5px"
  md: "7px"
  lg: "10px"
  xl: "12px"
  row: "8px"
  pill: "999px"
  full: "50%"
spacing:
  2xs: "4px"
  xs: "6px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "24px"
  toolbar-height: "52px"
  titlebar-height-non-mac: "36px"
  sidebar-width: "236px"
  sidebar-collapsed-width: "64px"
  list-width: "360px"
  mail-row-height: "92px"
  sidebar-row-height: "28px"
  reader-width: "760px"
  reader-gutter: "clamp(20px, 3vw, 36px)"
components:
  push-button:
    backgroundColor: "{colors.content}"
    textColor: "{colors.ink}"
    rounded: "{rounded.sm}"
    padding: "3px 12px"
    height: "26px"
  push-button-primary:
    backgroundColor: "{colors.system-blue}"
    textColor: "{colors.content}"
    rounded: "{rounded.sm}"
    padding: "3px 12px"
    height: "26px"
  toolbar-button:
    backgroundColor: "transparent"
    textColor: "{colors.ink-secondary}"
    rounded: "{rounded.sm}"
    size: "32px x 28px"
  source-list-row:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    rounded: "{rounded.sm}"
    padding: "4px 8px"
    height: "28px"
  source-list-row-selected:
    backgroundColor: "{colors.sidebar-active}"
    textColor: "{colors.ink}"
  search-field:
    backgroundColor: "{colors.content-secondary}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "0 8px"
    height: "28px"
  mail-row:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    rounded: "{rounded.row}"
    padding: "11px 12px 11px 24px"
    height: "92px"
  mail-row-selected-inactive:
    backgroundColor: "{colors.selection-inactive}"
    textColor: "{colors.ink}"
  mail-row-selected-focused:
    backgroundColor: "{colors.selection-fill}"
    textColor: "{colors.content}"
  segmented-control:
    backgroundColor: "rgb(29 29 31 / 7%)"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "2px"
  inset-group:
    backgroundColor: "{colors.content-secondary}"
    textColor: "{colors.ink}"
    rounded: "{rounded.lg}"
    padding: "14px 16px"
  remote-content-bar:
    backgroundColor: "{colors.signal-bg}"
    textColor: "{colors.signal-ink}"
    rounded: "{rounded.md}"
    padding: "8px 10px 8px 12px"
  sheet:
    backgroundColor: "{colors.content}"
    textColor: "{colors.ink}"
    rounded: "{rounded.xl}"
    padding: "20px 20px 16px"
  menu-item-highlighted:
    backgroundColor: "{colors.selection-fill}"
    textColor: "{colors.content}"
    rounded: "{rounded.sm}"
    padding: "3px 9px"
---

# Design System: GoMail

## Overview

**Creative North Star: "Clarity"**

GoMail looks and behaves like the mail client that ships with the desktop it runs on. The user asked for the logic of macOS Mail and Outlook, leaning towards macOS: three panes, a sidebar that runs to the top of the window, no status bar under the panes, restraint everywhere. Outlook contributes one thing on purpose: comfortable message rows with a sender avatar and two lines of preview, plus hover quick actions.

The world lends four things and only these: system type, a neutral palette with one system-blue accent, platform density (28px source-list rows, 52px toolbar rows), and one signature move. Navigation, controls and layout are the standard ones every desktop mail user already knows.

**Signature move — focus-aware selection.** The selected message row is a filled accent row with white text while the list has keyboard focus, and a quiet neutral row once focus moves into the reader or anywhere else — exactly how the platform's lists behave. It is implemented by re-pointing the colour tokens inside the selected row, so icons whose colour is set inline follow too.

**Key characteristics**
- One tinted plane: the sidebar (`#efeef2` / `#28282b`) runs from the window's top edge to its bottom, holding the traffic lights, the source list, and the sync status footer. List and reader are plain content white (`#ffffff` / `#1e1e20`).
- Every pane header is a 52px toolbar row that is also the window's title area: on macOS it carries `--app-region: drag`, so empty toolbar space moves the window.
- Hairlines (`--color-separator`) instead of borders; push buttons are a white face lifted by a hairline shadow; selection, menus and the settings source list highlight in the accent.
- Rejected on purpose: the old Harbor Chartroom chrome (brand title bar, full-width status horizon, terracotta signal strip, washed unread rows), colour-striped rows, uppercase eyebrow labels, decorative entrance motion.

## Colors

- **System blue** (`--color-accent` `#0064e0`; dark `#4a9eff`) marks primary actions, links, source-list symbols, unread dates and the focused selection. In dark mode text-and-icon blue and fill blue are separate tokens: `--color-accent` (`#4a9eff`, 4.6:1 on content) for text and icons, `--color-accent-fill` (`#1c6fe3`, 4.7:1 under white) for solid fills. Every solid accent fill uses `--color-accent-fill`.
- **Selection**: `--color-selection-fill` (`#0064e0` / `#0f5bd0`) with `--color-selection-text` white and `--color-selection-muted` (white 80–82%) for secondary text; `--color-selection-inactive` (`#e3e3e8` / `#3a3a3d`) when the list is not focused.
- **Neutrals**: ink `#1d1d1f`, secondary `#5c5c61` (6.4:1), tertiary `#737378` (4.7:1) on white; dark ink `#f5f5f7`, secondary `#ababb0`, tertiary `#929297` (5.1:1 on `#1e1e20`).
- **Signal amber** (`#c27400` on `#fff6df`, ink `#6f4500`; dark `#ffb340` on `#2f2615`) is reserved for operational truth: blocked remote content, offline, queued or failed remote writes, uncertain sends, error toasts.
- **Status**: success `#1a7f37`, warning `#9a5800`, error `#c4161c`, destructive buttons `#d70015`.

## Typography

One family: the platform system font (`-apple-system`/SF Pro on macOS, Segoe UI Variable on Windows) with PingFang SC / Microsoft YaHei UI for Chinese. Base size 13px, as native desktop UI. Fixed scale, no fluid type:

| Role | Size / weight |
|---|---|
| Pane toolbar title ("收件箱") | 15px / 700, with an 11px tertiary subtitle ("2 封邮件") |
| Settings pane title | 20px / 700 |
| Reader subject | 17px / 600 (second line of the header) |
| Reader sender | 14px / 700, address 12px tertiary |
| Reading body | 14px / 1.7 |
| Row sender / subject / preview | 13px 600 (700 unread) / 12.5px (600 unread) / 12px, two lines |
| Source-list rows | 13px / 400 (500 selected) |
| Section labels, meta, counts | 11–12px, tabular numerals |

## Layout & Shell

- **macOS**: no title bar row. `TrafficLightPosition {20, 19}` centres the window controls in the sidebar's 52px top row; the sidebar toolbar keeps `padding-left: var(--mygo-titlebar-inset-left)` and holds the sidebar toggle and the compose button at its trailing edge. Collapsed, the rail is at least as wide as the traffic lights and stacks the two controls under them.
- **Windows / Linux**: a slim 36px title bar row keeps the window controls; everything below is identical.
- **Sidebar order**: toolbar row → search field → Starred / Snoozed → "邮件" section → mailbox groups (account rows read as section headings with a 20px monogram, folders indented under them) → Contacts / Kanban / Settings → status footer (sync dot and text, realtime state, sync button, queued-write beacons, update pill).
- **Panes**: list `clamp(300px, 36%, 360px)`; reader fills the rest with a 760px reading column. With nothing selected the reader still shows its empty toolbar row and "未选择邮件".
- **Narrow (≤760px, selftest only — production minimum is 800px)**: list-or-reader; the reader shows a back control and pads its toolbar for the traffic lights when the sidebar is hidden.

## Components

- **Message rows**: inset 8px with 8px radius; 36px sender avatar tinted from the sender's address colour; sender + date, subject, two-line preview; 9px unread dot in the leading gutter; separators start at the text column and vanish next to the selection. Hover reveals archive / spam / kanban / star in the date's place.
- **Reader**: toolbar of bare 32×28 symbol buttons (reply group, then star/archive/delete/kanban, then the message tools at the trailing edge). Header: 44px avatar in the sender's colour; sender and date on line one, subject on line two, recipients on line three. The remote-content bar sits where the body starts.
- **Conversations**: each message is its own white sheet with a soft shadow on a light grey canvas.
- **Compose**: toolbar with back, title, "保存并关闭" push button and the blue send button; address rows separated by hairlines; editor mode as a segmented control; borderless writing surface.
- **Settings**: a white source list whose selected tab is accent-filled (System Settings grammar); sections are inset grouped boxes; text fields 1px strong hairline, 3px accent focus ring.
- **Overlays**: command palette is a translucent launcher with no backdrop dim; alerts are compact sheets with buttons at the trailing edge; toasts appear at the top trailing corner; context menus are translucent with accent-highlighted rows.

## Motion

120ms / 180ms with `cubic-bezier(0.25, 1, 0.5, 1)` for hover, selection and sidebar width. The only continuous motion is the sync dot pulsing while a sync pass runs. Reduced motion removes all of it.

## Preserved Constraints

- The stored interface-theme id stays `harbor` so existing profiles keep their preference; the display name is now "Clarity / 澄明".
- Message-detail reading templates remain independent of the interface theme.
- Availability guards, accessibility semantics, bilingual copy and the selftest class hooks (`.sidebar`, `.sidebar-row`, `.message-list-row`, `.message-row-subject`, `.app-statusbar`, `.app-reader-*`) are unchanged.
- Nothing loads from the network: system fonts and lucide icons only.
