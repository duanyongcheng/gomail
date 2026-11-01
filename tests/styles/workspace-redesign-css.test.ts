import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(join(process.cwd(), "src", "styles", "workspace-redesign.css"), "utf8");

describe("compose header action layout", () => {
  it("keeps save and send adjacent in a right-aligned action group", () => {
    const actions = css.match(/\.compose-workspace-actions\s*\{([^}]+)\}/)?.[1] ?? "";
    expect(actions).toMatch(/display:\s*flex/);
    expect(actions).toMatch(/align-items:\s*center/);
    expect(actions).toMatch(/justify-content:\s*flex-end/);
    expect(actions).toMatch(/gap:\s*8px/);
    expect(actions).toMatch(/margin-left:\s*auto/);
    expect(actions).not.toContain("space-between");
  });

  it("wraps the complete action group below the heading on narrow screens", () => {
    const narrow = css.slice(css.indexOf("@media (max-width: 760px)"));
    expect(narrow).toMatch(/\.compose-workspace-header\s*\{[^}]*flex-wrap:\s*wrap/);
    expect(narrow).toMatch(/\.compose-workspace-header > :first-child\s*\{[^}]*flex:\s*1 1 100%/);
    expect(narrow).toMatch(/\.compose-workspace-actions\s*\{[^}]*flex:\s*1 1 100%/);
    expect(narrow).toMatch(/\.compose-workspace-actions\s*\{[^}]*min-width:\s*0/);
    expect(narrow).toMatch(/\.compose-workspace-actions\s*\{[^}]*flex-wrap:\s*wrap/);
    expect(narrow).toMatch(/\.compose-save-button\s*\{[^}]*flex:\s*1/);
    expect(narrow).toMatch(/\.compose-send-button\s*\{[^}]*flex:\s*1/);
  });
});
