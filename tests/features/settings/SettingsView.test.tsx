import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SettingsView from "../../../src/features/settings/SettingsView";
import { useUIStore } from "../../../src/stores/ui.store";

vi.mock("react-i18next", () => ({
  initReactI18next: {
    type: "3rdParty",
    init: vi.fn(),
  },
  useTranslation: () => ({
    t: (key: string, fallback?: string) => {
      const labels: Record<string, string> = {
        "settings.accounts": "Accounts",
        "settings.general": "General",
        "settings.proxy": "Proxy",
        "settings.appearance": "Appearance",
        "settings.privacy": "Privacy",
        "settings.rules": "Rules",
        "settings.remoteWrites": "Remote Writes",
        "settings.translation": "Translation",
        "settings.shortcuts": "Shortcuts",
        "settings.cloudSync": "Settings Backup",
        "settings.about": "About",
        "settings.tabs": "Settings tabs",
      };
      return labels[key] ?? fallback ?? key;
    },
  }),
}));

vi.mock("../../../src/features/settings/AccountsTab", () => ({
  default: () => <div>Accounts panel</div>,
}));

vi.mock("../../../src/features/settings/GeneralTab", () => ({
  default: () => <div>General panel</div>,
}));

vi.mock("../../../src/features/settings/ProxyTab", () => ({
  default: () => <div>Proxy panel</div>,
}));

vi.mock("../../../src/features/settings/AppearanceTab", () => ({
  default: () => <div>Appearance panel</div>,
}));

vi.mock("../../../src/features/settings/CloudSyncTab", () => ({
  default: () => <div>Cloud sync panel</div>,
}));

vi.mock("../../../src/features/settings/RulesTab", () => ({
  default: () => <div>Rules panel</div>,
}));

vi.mock("../../../src/features/settings/ShortcutsTab", () => ({
  default: () => <div>Shortcuts panel</div>,
}));

vi.mock("../../../src/features/settings/TranslateTab", () => ({
  default: () => <div>Translation panel</div>,
}));

vi.mock("../../../src/features/settings/PrivacyTab", () => ({
  default: () => <div>Privacy panel</div>,
}));

vi.mock("../../../src/features/settings/AboutTab", () => ({
  default: () => <div>About panel</div>,
}));

vi.mock("../../../src/features/compose/OutboxPanel", () => ({ default: () => <div>Outbox panel</div> }));

vi.mock("../../../src/features/settings/PendingOpsTab", () => ({
  default: () => <div>Remote queue panel</div>,
}));

describe("SettingsView", () => {
  beforeEach(() => {
    useUIStore.setState({ settingsTab: "accounts" });
  });

  afterEach(() => { vi.unstubAllGlobals(); });

  it("exposes the pending remote writes queue as a settings tab", () => {
    render(<SettingsView />);

    const tab = screen.getByRole("tab", { name: "Remote Writes" });
    fireEvent.click(tab);

    expect(tab.getAttribute("aria-selected")).toBe("true");
    expect(screen.getByText("Remote queue panel")).toBeTruthy();
    expect(screen.getByText("Outbox panel")).toBeTruthy();
  });

  it("opens the implemented proxy settings from its enabled tab", () => {
    render(<SettingsView />);

    const tab = screen.getByRole("tab", { name: "Proxy" });
    fireEvent.click(tab);

    expect((tab as HTMLButtonElement).disabled).toBe(false);
    expect(tab.getAttribute("aria-selected")).toBe("true");
    expect(screen.getByText("Proxy panel")).toBeTruthy();
  });

  it.each(["rules", "translation", "ai", "cloudSync"] as const)("guards restored %s settings before mounting", (settingsTab) => {
    useUIStore.setState({ settingsTab });
    render(<SettingsView />);
    expect(screen.getByText("Accounts panel")).toBeTruthy();
    expect(useUIStore.getState().settingsTab).toBe("accounts");
    expect(screen.queryByText("Proxy panel")).toBeNull();
    expect(screen.queryByText("Rules panel")).toBeNull();
    expect(screen.queryByText("Cloud sync panel")).toBeNull();
  });

  it("restores the implemented proxy tab", () => {
    useUIStore.setState({ settingsTab: "proxy" });
    render(<SettingsView />);
    expect(screen.getByText("Proxy panel")).toBeTruthy();
    expect(useUIStore.getState().settingsTab).toBe("proxy");
  });

  it("keyboard tab navigation skips disabled settings", () => {
    render(<SettingsView />);
    fireEvent.keyDown(screen.getByRole("tab", { name: "General" }), { key: "ArrowDown" });
    expect(useUIStore.getState().settingsTab).toBe("proxy");
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Proxy" }));
  });

  it("updates the narrow tab orientation and skips disabled settings with horizontal keys", () => {
    let changed: (() => void) | undefined;
    const media = {
      matches: false,
      addEventListener: vi.fn((_event: string, listener: () => void) => { changed = listener; }),
      removeEventListener: vi.fn(),
    };
    vi.stubGlobal("matchMedia", vi.fn(() => media));
    const view = render(<SettingsView />);
    const tabs = screen.getByRole("tablist", { name: "Settings tabs" });
    expect(tabs.getAttribute("aria-orientation")).toBe("vertical");

    act(() => { media.matches = true; changed?.(); });
    expect(tabs.getAttribute("aria-orientation")).toBe("horizontal");
    fireEvent.keyDown(screen.getByRole("tab", { name: "General" }), { key: "ArrowRight" });
    expect(useUIStore.getState().settingsTab).toBe("proxy");
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Proxy" }));
    fireEvent.keyDown(document.activeElement!, { key: "ArrowLeft" });
    expect(useUIStore.getState().settingsTab).toBe("general");
    fireEvent.keyDown(document.activeElement!, { key: "End" });
    expect(useUIStore.getState().settingsTab).toBe("about");
    fireEvent.keyDown(document.activeElement!, { key: "Home" });
    expect(useUIStore.getState().settingsTab).toBe("accounts");
    expect(screen.getByRole("tab", { name: "Accounts" }).tabIndex).toBe(0);

    view.unmount();
    expect(media.removeEventListener).toHaveBeenCalledWith("change", changed);
  });

  it("does not animate all properties on settings tabs", () => {
    render(<SettingsView />);

    const tab = screen.getByRole("tab", { name: "Accounts" });

    expect(tab.style.transition).not.toContain("all");
    expect(tab.style.transition).toContain("background-color");
    expect(tab.style.transition).toContain("border-color");
  });

  it("keeps the settings panel vertically scrollable without horizontal overflow", () => {
    render(<SettingsView />);

    const panel = screen.getByRole("tabpanel");

    expect(panel.className).toContain("settings-panel-scroll");
    expect(panel.style.overflowY).toBe("auto");
    expect(panel.style.overflowX).toBe("hidden");
    expect(panel.style.boxSizing).toBe("border-box");
  });
});
