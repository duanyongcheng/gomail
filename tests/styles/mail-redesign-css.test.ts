import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(join(process.cwd(), "src/styles/mail-redesign.css"), "utf8");

describe("redesigned mail surfaces", () => {
  it("keeps the Harbor Chartroom list hierarchy and active focus isolation", () => {
    expect(css).toContain(".mail-list-heading");
    expect(css).toContain('[aria-selected="true"]');
    expect(css).toContain(".mail-state-center");
    expect(css).toContain("--mail-unread-marker-display: block");
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
