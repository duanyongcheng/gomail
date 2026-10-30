import { beforeEach, describe, expect, it, vi } from "vitest";
import { BACKGROUND_IMAGE_STORAGE_KEY, useUIStore } from "../../src/stores/ui.store";

const saved = { path: "/synthetic/wallpaper.webp", filename: "wallpaper.webp", fit: "cover" as const, opacity: 0.35, updatedAt: 1 };

describe("unavailable background image settings", () => {
  beforeEach(() => {
    localStorage.clear();
    useUIStore.setState({ backgroundImage: null });
  });

  it("does not claim an image was imported or persist unsupported edits", () => {
    useUIStore.getState().setBackgroundImage(saved);
    useUIStore.getState().setBackgroundImageFit("repeat");
    useUIStore.getState().setBackgroundImageOpacity(1);
    expect(useUIStore.getState().backgroundImage).toBeNull();
    expect(localStorage.getItem(BACKGROUND_IMAGE_STORAGE_KEY)).toBeNull();
  });

  it("preserves existing stored data rather than overwriting it with unsupported edits", () => {
    localStorage.setItem(BACKGROUND_IMAGE_STORAGE_KEY, JSON.stringify(saved));
    useUIStore.setState({ backgroundImage: saved });
    useUIStore.getState().setBackgroundImageFit("repeat");
    useUIStore.getState().setBackgroundImageOpacity(1);
    expect(useUIStore.getState().backgroundImage).toEqual(saved);
    expect(JSON.parse(localStorage.getItem(BACKGROUND_IMAGE_STORAGE_KEY)!)).toEqual(saved);
  });

  it("does not restore an unsupported background image but retains the preference", async () => {
    localStorage.setItem(BACKGROUND_IMAGE_STORAGE_KEY, JSON.stringify(saved));
    vi.resetModules();
    const { useUIStore: restored } = await import("../../src/stores/ui.store");
    expect(restored.getState().backgroundImage).toBeNull();
    expect(localStorage.getItem(BACKGROUND_IMAGE_STORAGE_KEY)).toBe(JSON.stringify(saved));
  });

  it("still allows clearing a local preference without claiming file deletion", () => {
    localStorage.setItem(BACKGROUND_IMAGE_STORAGE_KEY, JSON.stringify(saved));
    useUIStore.getState().clearBackgroundImage();
    expect(useUIStore.getState().backgroundImage).toBeNull();
    expect(localStorage.getItem(BACKGROUND_IMAGE_STORAGE_KEY)).toBeNull();
  });
});
