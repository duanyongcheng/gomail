import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AppearanceTab from "../../../src/features/settings/AppearanceTab";
import { useUIStore } from "../../../src/stores/ui.store";
import { deleteBackgroundImage, importBackgroundImage } from "../../../src/lib/backgroundImage";
import { unavailableReason } from "../../../src/lib/availability";

vi.mock("react-i18next", () => ({
  initReactI18next: { type: "3rdParty", init: vi.fn() },
  useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }),
}));
vi.mock("../../../src/lib/backgroundImage", () => ({
  backgroundImageUrl: vi.fn(), deleteBackgroundImage: vi.fn(), importBackgroundImage: vi.fn(),
}));

describe("AppearanceTab supported local preferences", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useUIStore.setState({ theme: "light", language: "en", backgroundImage: null });
  });

  it("explains unavailable background imports without exposing editable forms", () => {
    render(<AppearanceTab />);
    expect(screen.getByText(unavailableReason())).toBeTruthy();
    expect(screen.queryByLabelText("Choose background image")).toBeNull();
    expect(screen.queryByLabelText("Image opacity")).toBeNull();
    expect(importBackgroundImage).not.toHaveBeenCalled();
  });

  it("does not offer file removal or edits for a restored unsupported image", () => {
    useUIStore.setState({ backgroundImage: { path: "/synthetic/wallpaper.jpg", filename: "wallpaper.jpg", fit: "cover", opacity: 1, updatedAt: 1 } });
    render(<AppearanceTab />);
    expect(screen.queryByRole("button", { name: "Remove background image" })).toBeNull();
    expect(screen.queryByLabelText("Image opacity")).toBeNull();
    expect(deleteBackgroundImage).not.toHaveBeenCalled();
  });

  it("keeps local theme and language editing working", () => {
    render(<AppearanceTab />);
    fireEvent.click(screen.getByRole("button", { name: /settings.themeDark/ }));
    expect(useUIStore.getState().theme).toBe("dark");
    expect(localStorage.getItem("pebble-theme")).toBe("dark");
    fireEvent.click(screen.getByRole("button", { name: "English" }));
    expect(useUIStore.getState().language).toBe("en");
  });

  it("exposes the selected color mode without changing the independent interface theme", () => {
    useUIStore.setState({ appTheme: "clay" });
    render(<AppearanceTab />);
    const light = screen.getByRole("button", { name: /settings.themeLight/ });
    const dark = screen.getByRole("button", { name: /settings.themeDark/ });
    const system = screen.getByRole("button", { name: /settings.themeSystem/ });
    for (const [button, mode] of [[light, "light"], [dark, "dark"], [system, "system"]] as const) {
      expect(button.getAttribute("type")).toBe("button");
      expect(button.getAttribute("data-color-mode")).toBe(mode);
      expect(button.style.minWidth).toBe("0");
      expect(button.style.borderRadius).toBe("var(--ui-radius-md)");
    }
    expect(light.getAttribute("aria-pressed")).toBe("true");
    expect(dark.getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(dark);
    expect(light.getAttribute("aria-pressed")).toBe("false");
    expect(dark.getAttribute("aria-pressed")).toBe("true");
    expect(system.getAttribute("aria-pressed")).toBe("false");
    expect(useUIStore.getState().appTheme).toBe("clay");
  });

  it("allows the message-template grid to shrink below its preferred card width", () => {
    render(<AppearanceTab />);
    const templates = screen.getByRole("radiogroup", { name: "Message theme" });
    expect(templates.style.gridTemplateColumns).toBe("repeat(auto-fit, minmax(min(250px, 100%), 1fr))");
  });
});
