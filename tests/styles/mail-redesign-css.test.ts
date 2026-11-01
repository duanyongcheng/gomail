import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(join(process.cwd(), "src/styles/mail-redesign.css"), "utf8");

describe("redesigned mail surfaces", () => {
  it("keeps each direction's structural list treatment", () => {
    expect(css).toMatch(/\[data-ui-theme="clay"\]\s*\{[^}]*--mail-row-radius:\s*14px/s);
    expect(css).toMatch(/\[data-ui-theme="glass"\]\s*\{[^}]*--mail-avatar-display:\s*grid/s);
    expect(css).toMatch(/\[data-ui-theme="editorial"\]\s*\{[^}]*--mail-row-rule:\s*dotted/s);
    expect(css).toMatch(/\[data-ui-theme="minimal"\]\s*\{[^}]*--mail-index-display:\s*block/s);
  });

  it("uses measurable theme-driven row size rather than clipping at a fixed height", () => {
    const rowRule = css.match(/\[data-ui-theme\] \.mail-row\s*\{([^}]+)\}/)?.[1];
    expect(rowRule).toContain("min-height: var(--mail-row-height)");
    expect(rowRule).not.toMatch(/(?:^|;)\s*height:/);
    expect(css).toContain("--mail-row-height: var(--ui-mail-row-height, 88px)");
  });

  it("changes from split panes to list-or-reader on narrow screens", () => {
    const mobile = css.slice(css.indexOf("@media (max-width: 760px)"));
    expect(mobile).toMatch(/\.mail-reader-panel--empty\s*\{\s*display:\s*none/);
    expect(mobile).toMatch(/\.mail-workspace--detail-open \.mail-list-panel\s*\{\s*display:\s*none/);
    expect(mobile).toMatch(/\.mail-workspace--detail-open \.mail-reader-panel\s*\{\s*width:\s*100%/);
  });
});
