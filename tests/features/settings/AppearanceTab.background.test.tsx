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
});
