import { act, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AppThemePicker from "@/components/AppThemePicker";
import { APP_THEMES, type AppThemeId } from "@/lib/appThemes";
import { useUIStore } from "@/stores/ui.store";

vi.mock("react-i18next", () => ({
  initReactI18next: { type: "3rdParty", init: vi.fn() },
  useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }),
}));

function ControlledPicker() {
  const [activeTheme, setActiveTheme] = useState<AppThemeId>("harbor");
  return <AppThemePicker activeTheme={activeTheme} onSelect={setActiveTheme} />;
}

describe("AppThemePicker", () => {
  beforeEach(() => {
    useUIStore.setState({ theme: "light" });
  });

  it("renders the unified Clarity live three-pane preview without multi-theme radio chrome", () => {
    render(<AppThemePicker activeTheme="harbor" onSelect={vi.fn()} />);
    expect(screen.queryByRole("radiogroup")).toBeNull();
    expect(screen.queryByRole("radio")).toBeNull();
    const theme = APP_THEMES[0];
    const card = screen.getByRole("button", { name: theme.name });
    expect(card.getAttribute("data-app-theme-option")).toBe("harbor");
    expect(card.getAttribute("data-active")).toBe("true");
    const preview = card.querySelector<HTMLElement>(".app-theme-preview");
    expect(preview?.dataset.previewTheme).toBe("harbor");
    expect(card.querySelector(".app-theme-preview-sidebar")).toBeTruthy();
    expect(card.querySelector(".app-theme-preview-list")).toBeTruthy();
    expect(card.querySelector(".app-theme-preview-reader")).toBeTruthy();
    expect(preview?.style.getPropertyValue("--app-preview-accent")).toBe(theme.preview.light.accent);
    expect(screen.queryByText("Selected")).toBeNull();
    expect(card.querySelector(".app-theme-option-letter")).toBeNull();
    expect(card.querySelector(".app-theme-option-check")).toBeNull();
  });

  it("reports selection when clicked and supports controlled rendering", () => {
    const onSelect = vi.fn();
    render(<AppThemePicker activeTheme="harbor" onSelect={onSelect} />);
    fireEvent.click(screen.getByRole("button", { name: "Clarity" }));
    expect(onSelect).toHaveBeenCalledWith("harbor");
    render(<ControlledPicker />);
    expect(screen.getAllByRole("button", { name: "Clarity" })[1].getAttribute("data-active")).toBe("true");
  });

  it("updates the preview palette when the independent color mode changes", () => {
    render(<AppThemePicker activeTheme="harbor" onSelect={vi.fn()} />);
    const harbor = document.querySelector<HTMLElement>('[data-preview-theme="harbor"]')!;
    expect(harbor.style.getPropertyValue("--app-preview-background")).toBe(APP_THEMES[0].preview.light.background);
    // The live mode hook repaints the same preview, rather than remounting the picker.
    act(() => useUIStore.getState().setTheme("dark"));
    expect(harbor.style.getPropertyValue("--app-preview-background")).toBe(APP_THEMES[0].preview.dark.background);
    expect(harbor.style.getPropertyValue("--app-preview-accent")).toBe(APP_THEMES[0].preview.dark.accent);
  });
});
