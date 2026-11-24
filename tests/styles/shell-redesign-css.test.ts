import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(join(process.cwd(), "src", "styles", "shell-redesign.css"), "utf8");

describe("theme shell styling", () => {
  it("runs one tinted sidebar plane to the top of the window with the status at its foot", () => {
    expect(css).toMatch(/\.app-sidebar-column\s*\{[^}]*background:\s*var\(--color-sidebar-bg\)/);
    expect(css).toContain(".sidebar-topbar");
    expect(css).toContain(".sidebar-topbar-button");
    expect(css).toContain(".app-statusbar");
    expect(css).toContain(".app-offline-banner");
    expect(css).toContain('[data-ui-theme] .sidebar-account-row[data-selected="true"]');
    expect(css).toContain('[data-ui-theme] .sidebar-row[data-selected="true"]');
    // The status footer never widens the column: the sidebar sets its width.
    expect(css).toMatch(/\.app-statusbar\s*\{[^}]*width:\s*0;[^}]*min-width:\s*100%/);
  });

  it("keeps the traffic lights clear and lets every toolbar row drag the window on macOS", () => {
    expect(css).toMatch(/\.app-shell--mac \.sidebar-topbar\s*\{[^}]*padding-left:\s*var\(--mygo-titlebar-inset-left/);
    expect(css).toMatch(/\.app-shell--mac :is\([^)]*\.mail-list-heading[^)]*\)\s*\{[^}]*--app-region:\s*drag/);
    expect(css).toMatch(/\.app-shell--mac \.sidebar--collapsed\s*\{[^}]*var\(--mygo-titlebar-inset-left/);
  });

  it("lets the reader use the narrow viewport without removing folder navigation normally", () => {
    expect(css).toMatch(/@media\s*\(max-width:\s*760px\)/);
    expect(css).toContain(".app-shell-content:has(.mail-workspace--detail-open) > .app-sidebar-column");
    expect(css).toContain(".app-shell-content:has(.mail-workspace--detail-open) .sidebar");
    expect(css).toContain("min(var(--ui-sidebar-width, 236px), 42vw)");
  });

  it("preserves focus visibility and respects reduced motion", () => {
    expect(css).toContain(".app-window-button:focus-visible");
    expect(css).toContain("@media (prefers-reduced-motion: reduce)");
    expect(css).not.toContain("!important");
  });
});
