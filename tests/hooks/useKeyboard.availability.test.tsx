import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useKeyboard } from "../../src/hooks/useKeyboard";
import { DEFAULT_BINDINGS, useShortcutStore } from "../../src/stores/shortcut.store";
import { useUIStore } from "../../src/stores/ui.store";
import { useComposeStore } from "../../src/stores/compose.store";
import { useConfirmStore } from "../../src/stores/confirm.store";

const unsupported = ["toggle-view-kanban", "open-cloud-settings", "toggle-notifications", "translate-selection", "toggle-bilingual", "summarize-message"];

describe("keyboard product availability", () => {
  beforeEach(() => {
    localStorage.clear();
    useUIStore.setState({ activeView: "compose", settingsTab: "accounts", notificationsEnabled: false });
    useComposeStore.setState({ composeDirty: true, composeMode: "new", pendingView: null });
    useShortcutStore.setState({ bindings: { ...DEFAULT_BINDINGS }, recording: null });
  });

  it.each(unsupported)("cannot invoke restored/custom %s even from dirty compose", (action) => {
    useShortcutStore.setState({ bindings: { [action]: "Ctrl+Shift+U" } });
    const confirm = vi.spyOn(useConfirmStore.getState(), "confirm");
    const events = vi.fn();
    const eventNames = ["gomail:translate-selection", "gomail:toggle-bilingual", "gomail:ai-summarize"];
    eventNames.forEach((name) => document.addEventListener(name, events));
    const { unmount } = renderHook(() => useKeyboard());
    act(() => { document.dispatchEvent(new KeyboardEvent("keydown", { key: "u", ctrlKey: true, shiftKey: true, bubbles: true })); });
    expect(useUIStore.getState().activeView).toBe("compose");
    expect(useUIStore.getState().settingsTab).toBe("accounts");
    expect(useUIStore.getState().notificationsEnabled).toBe(false);
    expect(useComposeStore.getState().composeDirty).toBe(true);
    expect(confirm).not.toHaveBeenCalled();
    expect(events).not.toHaveBeenCalled();
    unmount();
    eventNames.forEach((name) => document.removeEventListener(name, events));
    confirm.mockRestore();
  });

  it("refuses recording/editing unavailable shortcuts and excludes them from reset defaults", () => {
    useShortcutStore.getState().resetToDefaults();
    for (const action of unsupported) {
      expect(useShortcutStore.getState().bindings[action]).toBeUndefined();
      useShortcutStore.getState().startRecording(action);
      useShortcutStore.getState().updateShortcut(action, "Ctrl+U");
    }
    expect(useShortcutStore.getState().recording).toBeNull();
    expect(localStorage.getItem("gomail-shortcuts")).toBeNull();
    expect(useShortcutStore.getState().bindings["open-search"]).toBe("Ctrl+Shift+F");
  });

  it("keeps basic search shortcuts usable", async () => {
    useUIStore.setState({ activeView: "inbox" });
    useComposeStore.setState({ composeDirty: false, composeMode: null });
    renderHook(() => useKeyboard());
    await act(async () => { document.dispatchEvent(new KeyboardEvent("keydown", { key: "f", ctrlKey: true, shiftKey: true, bubbles: true })); });
    expect(useUIStore.getState().activeView).toBe("search");
  });

  it("sanitizes persisted unsupported bindings without deleting the saved preference", async () => {
    localStorage.setItem("gomail-shortcuts", JSON.stringify({ "toggle-view-kanban": "U", "open-search": "Ctrl+U" }));
    vi.resetModules();
    const { useShortcutStore: restored } = await import("../../src/stores/shortcut.store");
    expect(restored.getState().bindings["toggle-view-kanban"]).toBeUndefined();
    expect(restored.getState().bindings["open-search"]).toBe("Ctrl+U");
    expect(localStorage.getItem("gomail-shortcuts")).toContain("toggle-view-kanban");
  });
});
