import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import GeneralTab from "../../../src/features/settings/GeneralTab";
import { getAutostartEnabled, setAutostartEnabled } from "../../../src/lib/api";
import { useUIStore } from "../../../src/stores/ui.store";
import { unavailableReason } from "../../../src/lib/availability";

vi.mock("../../../src/lib/api", () => ({
  showTestNotification: vi.fn(), openDefaultMailSettings: vi.fn(),
  getAutostartEnabled: vi.fn().mockResolvedValue(true), setAutostartEnabled: vi.fn(),
}));
vi.mock("react-i18next", () => ({
  initReactI18next: { type: "3rdParty", init: vi.fn() },
  useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }),
}));

describe("GeneralTab unavailable startup behavior", () => {
  beforeEach(() => { vi.clearAllMocks(); localStorage.clear(); });

  it("does not interpret the autostart startup stub as OS support", () => {
    render(<GeneralTab />);
    const checkbox = screen.getByRole("checkbox", { name: "Launch GoMail at system startup" }) as HTMLInputElement;
    expect(checkbox.disabled).toBe(true);
    expect(checkbox.checked).toBe(false);
    expect(checkbox.title).toBe(unavailableReason());
    fireEvent.click(checkbox);
    expect(getAutostartEnabled).not.toHaveBeenCalled();
    expect(setAutostartEnabled).not.toHaveBeenCalled();
  });

  it("cannot turn on startup-hidden from a stale restored preference", () => {
    useUIStore.setState({ startHiddenToTray: true });
    render(<GeneralTab />);
    const checkbox = screen.getByRole("checkbox", { name: "Start hidden to tray" }) as HTMLInputElement;
    expect(checkbox.disabled).toBe(true);
    expect(checkbox.checked).toBe(false);
    fireEvent.click(checkbox);
    expect(localStorage.getItem("gomail-start-hidden-to-tray")).toBeNull();
  });
});
