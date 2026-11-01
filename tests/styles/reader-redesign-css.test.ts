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

  it("differentiates paper, clay, glass, editorial and minimal reading surfaces", () => {
    for (const id of ["paper", "clay", "glass", "editorial", "minimal"]) {
      expect(css).toContain(`[data-ui-theme="${id}"]`);
    }
    expect(css).toMatch(/\[data-ui-theme="clay"\] \.app-reader-sheet\s*\{[^}]*border-radius:\s*18px/s);
    expect(css).toMatch(/\[data-ui-theme="editorial"\] \.app-reader-sender\s*\{[^}]*border-top:\s*1px solid var\(--color-text-primary\)/s);
  });
});
