import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(join(process.cwd(), "src", "styles", "reader-redesign.css"), "utf8");

describe("interface-native reader styles", () => {
  it("wraps long subjects only inside the new interface reader", () => {
    expect(css).toMatch(/\.app-reader-sheet \.message-detail-title\s*\{[^}]*white-space:\s*normal/s);
    expect(css).toMatch(/\.app-reader-sheet \.message-detail-title\s*\{[^}]*align-self:\s*stretch/s);
    expect(css).not.toMatch(/^\.message-detail-title\s*\{/m);
  });

  it("uses one scrollable reading document and a bounded responsive column", () => {
    expect(css).toMatch(/\.app-reader-scroll\s*\{[^}]*overflow:\s*auto/s);
    expect(css).toMatch(/\.app-reader-sheet\s*\{[^}]*max-width:\s*var\(--ui-reader-width/s);
    expect(css).toContain("@media (max-width: 520px)");
  });

  it("defines the Harbor Chartroom reader toolbar, privacy signal strip, and reading sheet", () => {
    expect(css).toContain(".app-reader-toolbar");
    expect(css).toContain(".privacy-banner");
    expect(css).toContain(".app-reader-sheet");
    expect(css).toContain(".app-reader-sender");
  });
});
