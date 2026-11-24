import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
  APP_THEMES,
  APP_THEME_STORAGE_KEY,
  DEFAULT_APP_THEME,
  getAppTheme,
  isAppThemeId,
  readStoredAppTheme,
} from "@/lib/appThemes";

describe("interface theme registry", () => {
  it("registers the unified Clarity design system under the stored harbor id", () => {
    expect(APP_THEMES.map((theme) => theme.id)).toEqual(["harbor"]);
    expect(APP_THEMES.map((theme) => theme.letter)).toEqual(["A"]);
    expect(APP_THEMES[0].rowHeight).toBe(92);
    expect(APP_THEMES[0].name).toBe("Clarity");
  });

  it("provides complete and distinct light and dark previews", () => {
    for (const theme of APP_THEMES) {
      for (const mode of ["light", "dark"] as const) {
        expect(Object.keys(theme.preview[mode]).sort()).toEqual([
          "accent", "background", "line", "list", "muted", "reader", "sidebar", "text",
        ]);
        for (const value of Object.values(theme.preview[mode])) expect(value).toMatch(/^#[\da-f]{6}$/i);
      }
      expect(theme.preview.light).not.toEqual(theme.preview.dark);
    }
  });

  it("keeps previews in sync with the live light and dark application palettes", () => {
    const css = readFileSync(resolve(process.cwd(), "src/styles/app-themes.css"), "utf8");
    const propertyByPreviewKey = {
      background: "--color-main-bg",
      sidebar: "--color-sidebar-bg",
      list: "--color-list-bg",
      reader: "--color-reader-bg",
      text: "--color-text-primary",
      muted: "--color-text-tertiary",
      line: "--color-border",
      accent: "--color-accent",
    } as const;
    const normalizeHex = (value: string) => value.length === 4
      ? `#${[...value.slice(1)].map((digit) => digit.repeat(2)).join("")}`
      : value.toLowerCase();
    for (const theme of APP_THEMES) {
      for (const mode of ["light", "dark"] as const) {
        const selector = `[data-ui-theme="${theme.id}"]${mode === "dark" ? '[data-theme="dark"]' : ""}`;
        const start = css.indexOf(`${selector} {`);
        expect(start, selector).toBeGreaterThanOrEqual(0);
        const declarations = css.slice(start, css.indexOf("}", start));
        for (const [key, property] of Object.entries(propertyByPreviewKey)) {
          const color = declarations.match(new RegExp(`${property}:\\s*(#[\\da-f]+);`, "i"))?.[1];
          expect(color, `${selector} ${property}`).toBeDefined();
          expect(theme.preview[mode][key as keyof typeof propertyByPreviewKey], `${selector} ${property}`)
            .toBe(normalizeHex(color!));
        }
      }
    }
  });

  it("accepts only registered theme ids", () => {
    for (const theme of APP_THEMES) expect(isAppThemeId(theme.id)).toBe(true);
    for (const invalid of [undefined, null, "", "dark", "A", "unknown", {}, []]) {
      expect(isAppThemeId(invalid)).toBe(false);
    }
  });

  it("resolves registered directions and safely defaults unknown, null or missing ids", () => {
    for (const theme of APP_THEMES) expect(getAppTheme(theme.id)).toBe(theme);
    for (const invalid of [undefined, null, "", "dark", "A", "unknown"]) {
      expect(getAppTheme(invalid)).toBe(APP_THEMES[0]);
      expect(getAppTheme(invalid).id).toBe(DEFAULT_APP_THEME);
    }
  });

  it("reads a saved direction and falls back safely without rewriting legacy or corrupt data", () => {
    const data = new Map<string, string>();
    const storage = { getItem: (key: string) => data.get(key) ?? null };
    expect(readStoredAppTheme(storage)).toBe(DEFAULT_APP_THEME);
    data.set(APP_THEME_STORAGE_KEY, "harbor");
    expect(readStoredAppTheme(storage)).toBe("harbor");
    data.set(APP_THEME_STORAGE_KEY, "editorial");
    expect(readStoredAppTheme(storage)).toBe(DEFAULT_APP_THEME);
    expect(data.get(APP_THEME_STORAGE_KEY)).toBe("editorial");
    data.set(APP_THEME_STORAGE_KEY, "not-a-theme");
    expect(readStoredAppTheme(storage)).toBe(DEFAULT_APP_THEME);
    expect(data.get(APP_THEME_STORAGE_KEY)).toBe("not-a-theme");
  });
});
