import { describe, expect, it, vi } from "vitest";
import { readStartHiddenToTrayPreference, shouldShowMainWindowOnStartup } from "../../src/lib/startupVisibility";
import { showMainWindow } from "../../src/lib/showMainWindow";
import { call } from "mygo-runtime";

vi.mock("mygo-runtime", () => ({ call: vi.fn().mockResolvedValue({}), event: vi.fn(), currentWindow: {} }));

describe("unsupported tray startup through production window adapter", () => {
  it("ignores stored tray startup and shows the existing window once", async () => {
    const storage = { getItem: vi.fn(() => "true") };
    expect(readStartHiddenToTrayPreference(storage)).toBe(false);
    expect(shouldShowMainWindowOnStartup(storage)).toBe(true);
    localStorage.setItem("pebble-start-hidden-to-tray", "true");
    await showMainWindow();
    expect(call).toHaveBeenCalledTimes(1);
    expect(call).toHaveBeenCalledWith("AppService.WindowAction", { action: "show", width: 0, height: 0, x: 0, y: 0 });
    expect(localStorage.getItem("pebble-start-hidden-to-tray")).toBe("true");
    localStorage.removeItem("pebble-start-hidden-to-tray");
  });
});
