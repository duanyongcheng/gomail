import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import GeneralTab from "../../../src/features/settings/GeneralTab";
import { showTestNotification } from "../../../src/lib/api";
import { useUIStore } from "../../../src/stores/ui.store";

vi.mock("../../../src/lib/api", () => ({
  showTestNotification: vi.fn().mockResolvedValue(undefined),
  openDefaultMailSettings: vi.fn().mockResolvedValue(undefined),
  getAutostartEnabled: vi.fn().mockResolvedValue(false),
  setAutostartEnabled: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("react-i18next", () => ({
  initReactI18next: {
    type: "3rdParty",
    init: vi.fn(),
  },
  useTranslation: () => ({
    t: (key: string, fallback?: string) => {
      const labels: Record<string, string> = {
        "settings.realtimeMode": "Realtime Mode",
        "settings.realtimeModeDesc": "Choose how aggressively GoMail checks for new mail.",
        "settings.realtimeModeRealtime": "Realtime (recommended)",
        "settings.realtimeModeBalanced": "Balanced",
        "settings.realtimeModeBattery": "Battery saver",
        "settings.realtimeModeManual": "Manual only",
        "settings.realtimeModeRealtimeDesc": "Gmail checks about every 10 seconds while recently active, then every 30 seconds while idle.",
        "settings.realtimeModeBalancedDesc": "Gmail checks about every 30 seconds in the foreground.",
        "settings.realtimeModeBatteryDesc": "Gmail checks about every 60 seconds in the foreground. Background checks slow to about every 2 minutes.",
        "settings.realtimeModeManualDesc": "Stops background checks. Use Sync now to run a single pass.",
        "settings.syncInterval": "Sync Interval",
        "settings.syncIntervalDesc": "How often to check for new messages (seconds)",
        "settings.notifications": "Notifications",
        "settings.enableNotifications": "Enable desktop notifications",
        "settings.testNotification": "Send test notification",
        "settings.testNotificationSent": "Test notification sent",
        "settings.testNotificationFailed": "Failed to send test notification",
        "settings.closeBehavior": "Close Behavior",
        "settings.quitOnClose": "Quit app when window is closed",
        "settings.startupBehavior": "Startup Behavior",
        "settings.startHiddenToTray": "Start hidden to tray",
        "settings.folderCounts": "Folder Counts",
        "settings.showUnreadCount": "Show unread count badges in sidebar",
        "settings.systemLabels": "System Labels",
        "settings.showSystemLabels": "Show system labels in sidebar (e.g. CATEGORY_*, IMPORTANT, STARRED, UNREAD)",
      };
      return labels[key] ?? fallback ?? key;
    },
  }),
}));

describe("GeneralTab realtime mode", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useUIStore.setState({
      pollInterval: 15,
      realtimeMode: "realtime",
      showFolderUnreadCount: false,
      showSystemLabels: false,
      notificationsEnabled: false,
      keepRunningInBackground: true,
      startHiddenToTray: false,
    });
  });

  it("defaults to realtime mode", () => {
    expect(useUIStore.getState().realtimeMode).toBe("realtime");
  });

  it("shows realtime strategy choices and persists selection", () => {
    render(<GeneralTab />);

    expect(screen.getByRole("button", { name: "Realtime (recommended)" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Balanced" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Battery saver" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Manual only" })).toBeTruthy();
    expect(screen.getByText("Gmail checks about every 10 seconds while recently active, then every 30 seconds while idle.")).toBeTruthy();
    expect(screen.getByText("Gmail checks about every 30 seconds in the foreground.")).toBeTruthy();
    expect(screen.getByText("Gmail checks about every 60 seconds in the foreground. Background checks slow to about every 2 minutes.")).toBeTruthy();
    expect(screen.getByText("Stops background checks. Use Sync now to run a single pass.")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Battery saver" }));

    expect(useUIStore.getState().realtimeMode).toBe("battery");
    expect(localStorage.getItem("gomail-realtime-mode")).toBe("battery");
  });

  it("keeps desktop notifications disabled without persisting a false success", () => {
    render(<GeneralTab />);

    const checkbox = screen.getByRole("checkbox", { name: "Enable desktop notifications" });
    expect((checkbox as HTMLInputElement).checked).toBe(false);
    expect((checkbox as HTMLInputElement).disabled).toBe(true);
    fireEvent.click(checkbox);
    expect(useUIStore.getState().notificationsEnabled).toBe(false);
    expect(localStorage.getItem("gomail-notifications-enabled")).toBeNull();
  });

  it("cannot send an unavailable desktop test notification", () => {
    render(<GeneralTab />);

    fireEvent.click(screen.getByRole("button", { name: "Send test notification" }));

    expect((screen.getByRole("button", { name: "Send test notification" }) as HTMLButtonElement).disabled).toBe(true);
    expect(showTestNotification).not.toHaveBeenCalled();
  });

  it("shows close-window behavior and persists quit-on-close through the UI store", () => {
    render(<GeneralTab />);

    const checkbox = screen.getByRole("checkbox", { name: "Quit app when window is closed" });
    expect((checkbox as HTMLInputElement).checked).toBe(false);

    fireEvent.click(checkbox);

    expect(useUIStore.getState().keepRunningInBackground).toBe(false);
    expect(localStorage.getItem("gomail-keep-running-background")).toBe("false");
  });

  it("does not persist unavailable start-hidden-to-tray behavior", () => {
    render(<GeneralTab />);

    const checkbox = screen.getByRole("checkbox", { name: "Start hidden to tray" });
    expect((checkbox as HTMLInputElement).checked).toBe(false);

    fireEvent.click(checkbox);

    expect((checkbox as HTMLInputElement).disabled).toBe(true);
    expect(useUIStore.getState().startHiddenToTray).toBe(false);
    expect(localStorage.getItem("gomail-start-hidden-to-tray")).toBeNull();
  });

  it("defaults sidebar system labels to unchecked and persists toggling them", () => {
    render(<GeneralTab />);

    const checkbox = screen.getByRole("checkbox", {
      name: "Show system labels in sidebar (e.g. CATEGORY_*, IMPORTANT, STARRED, UNREAD)",
    }) as HTMLInputElement;
    expect(checkbox.checked).toBe(false);

    fireEvent.click(checkbox);

    expect(useUIStore.getState().showSystemLabels).toBe(true);
    expect(localStorage.getItem("gomail-show-system-labels")).toBe("true");
  });
});
