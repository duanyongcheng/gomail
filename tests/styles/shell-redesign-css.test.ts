import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(join(process.cwd(), "src", "styles", "shell-redesign.css"), "utf8");

describe("theme shell styling", () => {
  it("provides the unified Harbor Chartroom chrome and token-driven sidebar layers", () => {
    expect(css).toContain(".sidebar-compose-button");
    expect(css).toContain(".app-statusbar");
    expect(css).toContain(".app-offline-banner");
    expect(css).toContain('[data-ui-theme] .sidebar-account-row[data-selected="true"]');
    expect(css).toContain('[data-ui-theme] .sidebar-row[data-selected="true"]');
  });

  it("lets the reader use the narrow viewport without removing folder navigation normally", () => {
    expect(css).toMatch(/@media\s*\(max-width:\s*760px\)/);
    expect(css).toContain(".app-shell-content:has(.mail-workspace--detail-open) > .sidebar");
    expect(css).toContain("min(var(--ui-sidebar-width, 244px), 42vw)");
  });

  it("preserves focus visibility and respects reduced motion", () => {
    expect(css).toContain(".sidebar-compose-button:focus-visible");
    expect(css).toContain(".app-window-button:focus-visible");
    expect(css).toContain("@media (prefers-reduced-motion: reduce)");
    expect(css).not.toContain("!important");
  });
});
