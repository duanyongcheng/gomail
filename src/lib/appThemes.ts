export type AppThemeId = "harbor";

export const APP_THEME_STORAGE_KEY = "gomail-app-theme";
export const DEFAULT_APP_THEME: AppThemeId = "harbor";

export interface AppThemePreviewPalette {
  background: string;
  sidebar: string;
  list: string;
  reader: string;
  text: string;
  muted: string;
  line: string;
  accent: string;
}

export interface AppTheme {
  id: AppThemeId;
  rowHeight: number;
  letter: string;
  name: string;
  labelKey: string;
  descriptionKey: string;
  description: string;
  preview: {
    light: AppThemePreviewPalette;
    dark: AppThemePreviewPalette;
  };
}

export const APP_THEMES: readonly AppTheme[] = [
  {
    id: "harbor",
    rowHeight: 92,
    letter: "A",
    name: "Clarity",
    labelKey: "appThemes.harbor",
    descriptionKey: "appThemes.harborDesc",
    description: "A native three-pane desktop layout: full-height sidebar, avatar message list, focus-aware selection, and a quiet white reading pane in light and dark.",
    preview: {
      light: {
        background: "#ffffff",
        sidebar: "#efeef2",
        list: "#ffffff",
        reader: "#ffffff",
        text: "#1d1d1f",
        muted: "#737378",
        line: "#e2e1e6",
        accent: "#0064e0",
      },
      dark: {
        background: "#1e1e20",
        sidebar: "#28282b",
        list: "#1e1e20",
        reader: "#1e1e20",
        text: "#f5f5f7",
        muted: "#929297",
        line: "#38383b",
        accent: "#4a9eff",
      },
    },
  },
];

export function isAppThemeId(value: unknown): value is AppThemeId {
  return APP_THEMES.some((theme) => theme.id === value);
}

export function getAppTheme(id: string | null | undefined): AppTheme {
  return APP_THEMES.find((theme) => theme.id === id) ?? APP_THEMES[0];
}

export function readStoredAppTheme(storage: Pick<Storage, "getItem">): AppThemeId {
  const stored = storage.getItem(APP_THEME_STORAGE_KEY);
  return isAppThemeId(stored) ? stored : DEFAULT_APP_THEME;
}

export function applyAppThemeToDom(theme: AppThemeId): void {
  if (typeof document !== "undefined") {
    const resolved = getAppTheme(theme);
    document.documentElement.setAttribute("data-ui-theme", resolved.id);
    document.documentElement.style.setProperty("--ui-mail-row-height", `${resolved.rowHeight}px`);
  }
}
