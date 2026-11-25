import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import ShortcutsTab from "../../../src/features/settings/ShortcutsTab";
import { useShortcutStore } from "../../../src/stores/shortcut.store";
import i18n from "../../../src/lib/i18n";

describe("ShortcutsTab supported actions", () => {
  beforeEach(async () => {
    await i18n.changeLanguage("en");
    useShortcutStore.getState().resetToDefaults();
    useShortcutStore.getState().stopRecording();
  });
  it("omits unavailable actions while retaining supported AI, translation, and local shortcut editing", () => {
    const { container } = render(<ShortcutsTab />);
    for (const action of ["toggle-view-kanban", "open-cloud-settings", "toggle-notifications"]) {
      expect(container.querySelector(`[data-action="${action}"]`)).toBeNull();
    }
    for (const action of ["translate-selection", "toggle-bilingual", "summarize-message"]) {
      expect(container.querySelector(`[data-action="${action}"]`)).not.toBeNull();
    }
    const row = container.querySelector('[data-action="open-search"]')!;
    fireEvent.click(row.querySelector("button")!);
    expect(useShortcutStore.getState().recording).toBe("open-search");
    fireEvent.keyDown(document, { key: "u", ctrlKey: true });
    expect(useShortcutStore.getState().bindings["open-search"]).toBe("Ctrl+U");
    expect(useShortcutStore.getState().recording).toBeNull();
    expect(screen.getByText("Ctrl+U")).toBeTruthy();
  });
});
