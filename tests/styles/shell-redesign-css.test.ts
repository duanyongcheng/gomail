import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(join(process.cwd(), "src", "styles", "shell-redesign.css"), "utf8");

describe("theme shell styling", () => {
  it("gives all five directions their own structural treatment", () => {
    for (const theme of ["paper", "clay", "glass", "editorial", "minimal"]) {
      expect(css).toContain(`[data-ui-theme="${theme}"]`);
    }
    expect(css).toContain("counter(folder-index, decimal-leading-zero)");
    expect(css).toContain("var(--ui-shadow-sm, none)");
    expect(css).toContain("0 0 8px var(--color-accent)");
  });

  it("lets the reader use the narrow viewport without removing folder navigation normally", () => {
    expect(css).toMatch(/@media\s*\(max-width:\s*760px\)/);
    expect(css).toContain(".app-shell-content:has(.mail-workspace--detail-open) > .sidebar");
    expect(css).toContain("min(var(--ui-sidebar-width, 232px), 42vw)");
  });

  it("keeps Midnight Glass dark mailbox groups and selected accounts dark", () => {
    expect(css).toMatch(/\[data-ui-theme="glass"\]\[data-theme="dark"\]\s+\.sidebar-mailbox-group:not\(\.sidebar-mailbox-group--combined\)\s*\{[^}]*background\s*:\s*var\(--color-sidebar-bg\)/i);
    expect(css).toMatch(/\[data-ui-theme="glass"\]\[data-theme="dark"\]\s+\.sidebar-account-row\[data-selected="true"\]\s*\{[^}]*background\s*:\s*var\(--color-sidebar-active\)/i);
  });

  it("preserves focus visibility and respects reduced motion", () => {
    expect(css).toContain(".sidebar-compose-button:focus-visible");
    expect(css).toContain(".app-window-button:focus-visible");
    expect(css).toContain("@media (prefers-reduced-motion: reduce)");
    expect(css).not.toContain("!important");
  });

  it("uses non-transitioning dark layers for Midnight Glass mailbox and folder selection", () => {
    expect(css).toMatch(/\[data-ui-theme="glass"\]\[data-theme="dark"\]\s+\.sidebar-account-row,\s*\[data-ui-theme="glass"\]\[data-theme="dark"\]\s+\.sidebar-row\s*\{[^}]*transition\s*:\s*none/i);
    expect(css).toMatch(/\[data-ui-theme="glass"\]\[data-theme="dark"\]\s+\.sidebar-row\[data-selected="true"\]\s*\{[^}]*color-mix\(in srgb,\s*var\(--color-accent\)\s+16%,\s*var\(--color-sidebar-bg\)\)/i);
  });
});
