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
  const [activeTheme, setActiveTheme] = useState<AppThemeId>("paper");
  return <AppThemePicker activeTheme={activeTheme} onSelect={setActiveTheme} />;
}

describe("AppThemePicker", () => {
  beforeEach(() => {
    useUIStore.setState({ theme: "light" });
  });

  it("offers all five directions with a unique three-pane preview", () => {
    render(<AppThemePicker activeTheme="paper" onSelect={vi.fn()} />);
    expect(screen.getByRole("radiogroup", { name: "Interface theme" })).toBeTruthy();
    expect(screen.getAllByRole("radio")).toHaveLength(5);
    const previewStyles = new Set<string>();
    for (const theme of APP_THEMES) {
      const option = screen.getByRole("radio", { name: `${theme.letter} ${theme.name}` });
      const preview = option.querySelector<HTMLElement>(".app-theme-preview");
      expect(preview?.dataset.previewTheme).toBe(theme.id);
      expect(option.querySelector(".app-theme-preview-sidebar")).toBeTruthy();
      expect(option.querySelector(".app-theme-preview-list")).toBeTruthy();
      expect(option.querySelector(".app-theme-preview-reader")).toBeTruthy();
      expect(preview?.style.getPropertyValue("--app-preview-accent")).toBe(theme.preview.light.accent);
      previewStyles.add(preview?.getAttribute("style") ?? "");
    }
    expect(previewStyles.size).toBe(5);
  });

  it("marks only the selected option as checked and tabbable", () => {
    render(<AppThemePicker activeTheme="editorial" onSelect={vi.fn()} />);
    const selected = screen.getByRole("radio", { name: "D Editorial" });
    expect(selected.getAttribute("aria-checked")).toBe("true");
    expect(selected.tabIndex).toBe(0);
    expect(screen.getAllByRole("radio").filter((radio) => radio.tabIndex === 0)).toHaveLength(1);
    expect(screen.getByText("Selected")).toBeTruthy();
  });

  it("reports selections without closing the comparison", () => {
    const onSelect = vi.fn();
    render(<AppThemePicker activeTheme="paper" onSelect={onSelect} />);
    fireEvent.click(screen.getByRole("radio", { name: "B Warm Clay" }));
    expect(onSelect).toHaveBeenCalledWith("clay");
    expect(screen.getAllByRole("radio")).toHaveLength(5);
  });

  it("supports arrow navigation, wrapping, Home and End with roving focus", () => {
    render(<ControlledPicker />);
    const paper = screen.getByRole("radio", { name: "A Quiet Paper" });
    const clay = screen.getByRole("radio", { name: "B Warm Clay" });
    const minimal = screen.getByRole("radio", { name: "E Minimal Tool" });
    paper.focus();
    fireEvent.keyDown(paper, { key: "ArrowRight" });
    expect(document.activeElement).toBe(clay);
    expect(clay.getAttribute("aria-checked")).toBe("true");
    fireEvent.keyDown(clay, { key: "End" });
    expect(document.activeElement).toBe(minimal);
    expect(minimal.getAttribute("aria-checked")).toBe("true");
    fireEvent.keyDown(minimal, { key: "ArrowDown" });
    expect(document.activeElement).toBe(paper);
    fireEvent.keyDown(paper, { key: "ArrowLeft" });
    expect(document.activeElement).toBe(minimal);
    fireEvent.keyDown(minimal, { key: "Home" });
    expect(document.activeElement).toBe(paper);
    expect(paper.tabIndex).toBe(0);
    expect(minimal.tabIndex).toBe(-1);
  });

  it("updates every preview when the independent color mode changes", () => {
    render(<AppThemePicker activeTheme="glass" onSelect={vi.fn()} />);
    const glass = document.querySelector<HTMLElement>('[data-preview-theme="glass"]')!;
    expect(glass.style.getPropertyValue("--app-preview-background")).toBe(APP_THEMES[2].preview.light.background);
    // The live mode hook repaints the same preview, rather than remounting the picker.
    act(() => useUIStore.getState().setTheme("dark"));
    expect(glass.style.getPropertyValue("--app-preview-background")).toBe(APP_THEMES[2].preview.dark.background);
    expect(screen.getByRole("radio", { name: "C Midnight Glass" }).getAttribute("aria-checked")).toBe("true");
  });
});
