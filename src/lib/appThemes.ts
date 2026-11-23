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
    rowHeight: 88,
    letter: "A",
    name: "Harbor Chartroom",
    labelKey: "appThemes.harbor",
    descriptionKey: "appThemes.harborDesc",
    description: "Layered chartroom panes, high-contrast reading surface, marine-cerulean focus, and terracotta privacy strip—adapting cleanly across light and dark modes.",
    preview: {
      light: {
        background: "#f7fafb",
        sidebar: "#ebf1f4",
        list: "#f7fafb",
        reader: "#ffffff",
        text: "#142538",
        muted: "#526a80",
        line: "#d4e0e7",
        accent: "#155e8a",
      },
      dark: {
        background: "#131e2a",
        sidebar: "#0e1721",
        list: "#131e2a",
        reader: "#172432",
        text: "#ebf2f7",
        muted: "#88a4bc",
        line: "#25384b",
        accent: "#62b3e3",
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
