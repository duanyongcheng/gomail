import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

function readStyle(name: string): string {
  return readFileSync(join(process.cwd(), "src", "styles", name), "utf8");
}

describe("non-selectable UI chrome CSS", () => {
  it("disables text selection on buttons, sidebar navigation, and batch labels", () => {
    const indexCss = readStyle("index.css");
    const shellCss = readStyle("shell-redesign.css");

    expect(indexCss).toMatch(/button,\s*\.sidebar\s*\{[^}]*-webkit-user-select:\s*none;[^}]*user-select:\s*none/i);
    expect(indexCss).toMatch(/\.batch-select-control\s*\{[^}]*-webkit-user-select:\s*none;[^}]*user-select:\s*none/i);
    expect(shellCss).toMatch(/\.app-sidebar-column\s*\{[^}]*-webkit-user-select:\s*none;[^}]*user-select:\s*none/i);
  });

  it("disables text selection on message rows, context menus, and workspace navigation chrome", () => {
    const mailCss = readStyle("mail-redesign.css");
    const workspaceCss = readStyle("workspace-redesign.css");

    expect(mailCss).toMatch(/\[data-ui-theme\] \.mail-row\s*\{[^}]*-webkit-user-select:\s*none;[^}]*user-select:\s*none/i);
    expect(workspaceCss).toMatch(/\.folder-menu-surface\s*\{[^}]*-webkit-user-select:\s*none;[^}]*user-select:\s*none/i);
    expect(workspaceCss).toMatch(/\.settings-navigation\s*\{[^}]*-webkit-user-select:\s*none;[^}]*user-select:\s*none/i);
    expect(workspaceCss).toMatch(/\.search-result-row\s*\{[^}]*-webkit-user-select:\s*none;[^}]*user-select:\s*none/i);
    expect(workspaceCss).toMatch(/\.snoozed-row\s*\{[^}]*-webkit-user-select:\s*none;[^}]*user-select:\s*none/i);
    expect(workspaceCss).toMatch(/\.command-palette-list\s*\{[^}]*-webkit-user-select:\s*none;[^}]*user-select:\s*none/i);
  });
});
