import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(join(process.cwd(), "src/styles/mail-redesign.css"), "utf8");

describe("redesigned mail surfaces", () => {
  it("keeps the Clarity list hierarchy and focus-aware selection", () => {
    expect(css).toContain(".mail-list-heading");
    expect(css).toContain('[aria-selected="true"]');
    expect(css).toContain(".mail-state-center");
    expect(css).toContain("--mail-unread-marker-display: block");
    // Selection follows focus: neutral by default, the accent while the list has focus.
    expect(css).toMatch(/\.mail-row:is\(\.message-list-row, \.thread-list-row\)\[aria-selected="true"\]\s*\{[^}]*background:\s*var\(--color-selection-inactive\)/);
    expect(css).toMatch(/\[role="listbox"\]:focus-within \.mail-row[^{]*\[aria-selected="true"\]\s*\{[^}]*background:\s*var\(--color-selection-fill\)/);
  });

  it("shows sender avatars and two lines of preview in comfortable rows", () => {
    expect(css).toContain("--mail-avatar-display: grid");
    expect(css).toMatch(/\.mail-row-snippet\s*\{[^}]*-webkit-line-clamp:\s*2/);
  });

  it("uses measurable theme-driven row size rather than clipping at a fixed height", () => {
    const rowRule = css.match(/\[data-ui-theme\] \.mail-row\s*\{([^}]+)\}/)?.[1];
    expect(rowRule).toContain("min-height: var(--mail-row-height)");
    expect(rowRule).not.toMatch(/(?:^|;)\s*height:/);
    expect(css).toContain("--mail-row-height: var(--ui-mail-row-height, 92px)");
  });

  it("changes from split panes to list-or-reader on narrow screens", () => {
    const mobile = css.slice(css.indexOf("@media (max-width: 760px)"));
    expect(mobile).toMatch(/\.mail-reader-panel--empty\s*\{\s*display:\s*none/);
    expect(mobile).toMatch(/\.mail-workspace--detail-open \.mail-list-panel\s*\{\s*display:\s*none/);
    expect(mobile).toMatch(/\.mail-workspace--detail-open \.mail-reader-panel\s*\{\s*width:\s*100%/);
  });
});
