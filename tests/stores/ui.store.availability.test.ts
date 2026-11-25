import { beforeEach, describe, expect, it, vi } from "vitest";
import { useUIStore } from "../../src/stores/ui.store";
import { useComposeStore } from "../../src/stores/compose.store";

describe("UI store availability", () => {
  beforeEach(() => {
    localStorage.clear(); sessionStorage.clear();
    useUIStore.setState({ activeView: "inbox", settingsTab: "accounts", pendingContactId: null });
  });
  it.each(["contacts", "kanban", "snoozed"] as const)("refuses navigation to %s without leaving dirty compose", (view) => {
    useUIStore.setState({ activeView: "compose" });
    useComposeStore.setState({ composeDirty: true, pendingView: null, showComposeLeaveConfirm: false });
    useUIStore.getState().setActiveView(view);
    expect(useUIStore.getState().activeView).toBe("compose");
    expect(useComposeStore.getState().pendingView).toBeNull();
    expect(useComposeStore.getState().showComposeLeaveConfirm).toBe(false);
  });
  it("refuses contact deep links", () => {
    useUIStore.getState().openContactInContacts("synthetic");
    expect(useUIStore.getState().activeView).toBe("inbox");
    expect(useUIStore.getState().pendingContactId).toBeNull();
  });
  it.each(["rules", "translation", "cloudSync", "unknown"])("does not restore unsupported settings tab %s", async (tab) => {
    sessionStorage.setItem("gomail-settings-tab", tab);
    vi.resetModules();
    const { useUIStore: restored } = await import("../../src/stores/ui.store");
    expect(restored.getState().settingsTab).toBe("accounts");
  });
  it("restores the implemented ai settings tab", async () => {
    sessionStorage.setItem("gomail-settings-tab", "ai");
    vi.resetModules();
    const { useUIStore: restored } = await import("../../src/stores/ui.store");
    expect(restored.getState().settingsTab).toBe("ai");
  });
  it("restores the implemented proxy settings tab", async () => {
    sessionStorage.setItem("gomail-settings-tab", "proxy");
    vi.resetModules();
    const { useUIStore: restored } = await import("../../src/stores/ui.store");
    expect(restored.getState().settingsTab).toBe("proxy");
  });
  it("retains supported restored settings and disables unsupported startup preferences", async () => {
    sessionStorage.setItem("gomail-settings-tab", "privacy");
    localStorage.setItem("gomail-notifications-enabled", "true");
    localStorage.setItem("gomail-start-hidden-to-tray", "true");
    vi.resetModules();
    const { useUIStore: restored } = await import("../../src/stores/ui.store");
    expect(restored.getState().settingsTab).toBe("privacy");
    expect(restored.getState().notificationsEnabled).toBe(false);
    expect(restored.getState().startHiddenToTray).toBe(false);
    expect(localStorage.getItem("gomail-start-hidden-to-tray")).toBe("true");
  });
});
