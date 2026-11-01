export type AppThemeId = "paper" | "clay" | "glass" | "editorial" | "minimal";

export const APP_THEME_STORAGE_KEY = "pebble-app-theme";
export const DEFAULT_APP_THEME: AppThemeId = "paper";

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
    id: "paper",
    rowHeight: 88,
    letter: "A",
    name: "Quiet Paper",
    labelKey: "appThemes.paper",
    descriptionKey: "appThemes.paperDesc",
    description: "Warm paper, serif headings and fine rules. A quiet place for long reads.",
    preview: {
      light: { background: "#faf9f6", sidebar: "#faf9f6", list: "#faf9f6", reader: "#ffffff", text: "#23211d", muted: "#827b70", line: "#e8e4db", accent: "#b8432a" },
      dark: { background: "#22211d", sidebar: "#1e1d1a", list: "#22211d", reader: "#272520", text: "#efeae0", muted: "#a49b8b", line: "#39362f", accent: "#ed967b" },
    },
  },
  {
    id: "clay",
    rowHeight: 96,
    letter: "B",
    name: "Warm Clay",
    labelKey: "appThemes.clay",
    descriptionKey: "appThemes.clayDesc",
    description: "Terracotta warmth, soft corners and gently raised cards.",
    preview: {
      light: { background: "#fdfaf7", sidebar: "#fbf2ea", list: "#fffdfb", reader: "#fdfaf7", text: "#2a211c", muted: "#8a7669", line: "#efe2d7", accent: "#b35c39" },
      dark: { background: "#221b18", sidebar: "#281f1b", list: "#2a211c", reader: "#221b18", text: "#f5e8dd", muted: "#b49a85", line: "#46342a", accent: "#efa47b" },
    },
  },
  {
    id: "glass",
    rowHeight: 84,
    letter: "C",
    name: "Midnight Glass",
    labelKey: "appThemes.glass",
    descriptionKey: "appThemes.glassDesc",
    description: "Layered panels, electric blue and precise, compact information.",
    preview: {
      light: { background: "#f5f7fc", sidebar: "#edf0f7", list: "#f5f7fc", reader: "#ffffff", text: "#192238", muted: "#6a7893", line: "#d8deeb", accent: "#3d5fcd" },
      dark: { background: "#0a0b0d", sidebar: "#101216", list: "#15181d", reader: "#1b1f26", text: "#eef1f5", muted: "#8f9aad", line: "#2b3039", accent: "#6c8cff" },
    },
  },
  {
    id: "editorial",
    rowHeight: 100,
    letter: "D",
    name: "Editorial",
    labelKey: "appThemes.editorial",
    descriptionKey: "appThemes.editorialDesc",
    description: "Magazine-like typography, bold rules and a red editorial mark.",
    preview: {
      light: { background: "#ffffff", sidebar: "#f4f2ee", list: "#ffffff", reader: "#ffffff", text: "#111111", muted: "#7a756d", line: "#d9d5cc", accent: "#c8341f" },
      dark: { background: "#191918", sidebar: "#22211f", list: "#191918", reader: "#1d1c1a", text: "#f3efe7", muted: "#a69d8e", line: "#454139", accent: "#ff9782" },
    },
  },
  {
    id: "minimal",
    rowHeight: 68,
    letter: "E",
    name: "Minimal Tool",
    labelKey: "appThemes.minimal",
    descriptionKey: "appThemes.minimalDesc",
    description: "Neutral surfaces, tight rows and a single blue accent. Just the essentials.",
    preview: {
      light: { background: "#ffffff", sidebar: "#f4f5f7", list: "#ffffff", reader: "#ffffff", text: "#14161a", muted: "#707a8b", line: "#dfe2e7", accent: "#2f6bff" },
      dark: { background: "#101318", sidebar: "#191c21", list: "#101318", reader: "#14181e", text: "#e7ebf2", muted: "#909eb3", line: "#2e3540", accent: "#8bafff" },
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
    document.documentElement.setAttribute("data-ui-theme", theme);
    document.documentElement.style.setProperty("--ui-mail-row-height", `${getAppTheme(theme).rowHeight}px`);
  }
}
