import { beforeEach, describe, expect, it, vi } from "vitest";
import { APP_THEMES, APP_THEME_STORAGE_KEY } from "@/lib/appThemes";

describe("UI store interface themes", () => {
  beforeEach(() => {
    vi.resetModules();
    localStorage.clear();
    sessionStorage.clear();
    document.documentElement.removeAttribute("data-ui-theme");
    document.documentElement.removeAttribute("data-theme");
    document.documentElement.style.removeProperty("--ui-mail-row-height");
  });

  it("defaults to Harbor Chartroom and applies it before the first render", async () => {
    const { useUIStore } = await import("@/stores/ui.store");
    expect(useUIStore.getState().appTheme).toBe("harbor");
    expect(document.documentElement.getAttribute("data-ui-theme")).toBe("harbor");
    expect(document.documentElement.style.getPropertyValue("--ui-mail-row-height")).toBe("88px");
    expect(localStorage.getItem(APP_THEME_STORAGE_KEY)).toBeNull();
  });

  it.each(APP_THEMES.map((theme) => theme.id))("restores %s and applies it at startup", async (theme) => {
    localStorage.setItem(APP_THEME_STORAGE_KEY, theme);
    const { useUIStore } = await import("@/stores/ui.store");
    expect(useUIStore.getState().appTheme).toBe(theme);
    expect(document.documentElement.getAttribute("data-ui-theme")).toBe(theme);
    expect(document.documentElement.style.getPropertyValue("--ui-mail-row-height")).toBe(`${APP_THEMES.find((option) => option.id === theme)?.rowHeight}px`);
  });

  it("preserves a legacy or damaged stored preference while falling back to Harbor", async () => {
    localStorage.setItem(APP_THEME_STORAGE_KEY, "unrecognized");
    const { useUIStore } = await import("@/stores/ui.store");
    expect(useUIStore.getState().appTheme).toBe("harbor");
    expect(document.documentElement.getAttribute("data-ui-theme")).toBe("harbor");
    expect(localStorage.getItem(APP_THEME_STORAGE_KEY)).toBe("unrecognized");
  });

  it("keeps application style independent of color mode and message template", async () => {
    localStorage.setItem("pebble-theme", "dark");
    localStorage.setItem("pebble-message-theme", "telegram");
    const { useUIStore, applyThemeToDom } = await import("@/stores/ui.store");
    applyThemeToDom(useUIStore.getState().theme);
    useUIStore.getState().setAppTheme("harbor");
    expect(useUIStore.getState()).toMatchObject({ appTheme: "harbor", theme: "dark", messageTheme: "telegram" });
    expect(document.documentElement.getAttribute("data-ui-theme")).toBe("harbor");
    expect(document.documentElement.style.getPropertyValue("--ui-mail-row-height")).toBe("88px");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    expect(localStorage.getItem(APP_THEME_STORAGE_KEY)).toBe("harbor");
    expect(localStorage.getItem("pebble-theme")).toBe("dark");
    expect(localStorage.getItem("pebble-message-theme")).toBe("telegram");

    useUIStore.getState().setTheme("light");
    expect(useUIStore.getState().appTheme).toBe("harbor");
    expect(document.documentElement.getAttribute("data-ui-theme")).toBe("harbor");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });

  it("ignores invalid runtime writes", async () => {
    const { useUIStore } = await import("@/stores/ui.store");
    useUIStore.getState().setAppTheme("harbor");
    useUIStore.getState().setAppTheme("unrecognized" as never);
    expect(useUIStore.getState().appTheme).toBe("harbor");
    expect(localStorage.getItem(APP_THEME_STORAGE_KEY)).toBe("harbor");
    expect(document.documentElement.getAttribute("data-ui-theme")).toBe("harbor");
  });

  it("keeps theme writes and restoration scoped to the active profile", async () => {
    const { initializeProfileStorageNamespace } = await import("@/lib/profileStorage");
    await initializeProfileStorageNamespace(async () => "theme-profile-a");
    localStorage.setItem("pebble:profile:theme-profile-a:pebble-app-theme", "editorial");
    const { useUIStore } = await import("@/stores/ui.store");
    expect(useUIStore.getState().appTheme).toBe("harbor");
    useUIStore.getState().setAppTheme("harbor");
    expect(localStorage.getItem("pebble:profile:theme-profile-a:pebble-app-theme")).toBe("harbor");
    expect(localStorage.getItem(APP_THEME_STORAGE_KEY)).toBeNull();

    await initializeProfileStorageNamespace(async () => "theme-profile-b");
    useUIStore.getState().setAppTheme("harbor");
    expect(localStorage.getItem("pebble:profile:theme-profile-b:pebble-app-theme")).toBe("harbor");
    expect(localStorage.getItem("pebble:profile:theme-profile-a:pebble-app-theme")).toBe("harbor");
  });
});
