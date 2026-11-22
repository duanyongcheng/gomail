import { useId, type CSSProperties } from "react";
import { useTranslation } from "react-i18next";
import { APP_THEMES, type AppThemeId, type AppThemePreviewPalette } from "@/lib/appThemes";
import { useResolvedAppTheme } from "@/hooks/useResolvedAppTheme";

interface AppThemePickerProps {
  activeTheme: AppThemeId;
  onSelect: (theme: AppThemeId) => void;
}

function previewStyle(palette: AppThemePreviewPalette): CSSProperties {
  return Object.fromEntries(
    Object.entries(palette).map(([name, value]) => [`--app-preview-${name}`, value]),
  ) as CSSProperties;
}

export default function AppThemePicker({ activeTheme, onSelect }: AppThemePickerProps) {
  const { t } = useTranslation();
  const mode = useResolvedAppTheme();
  const id = useId();

  return (
    <div
      className="app-theme-picker"
      aria-label={t("appThemes.title", "Interface theme")}
    >
      {APP_THEMES.map((theme) => {
        const selected = activeTheme === theme.id;
        return (
          <button
            key={theme.id}
            type="button"
            aria-label={t(theme.labelKey, theme.name)}
            aria-describedby={`${id}-${theme.id}-description`}
            className="app-theme-option"
            data-app-theme-option={theme.id}
            data-active={selected ? "true" : "false"}
            onClick={() => onSelect(theme.id)}
          >
            <div
              className="app-theme-preview"
              data-preview-theme={theme.id}
              style={previewStyle(theme.preview[mode])}
              aria-hidden="true"
            >
              <div className="app-theme-preview-titlebar"><i /><i /><i /><span /></div>
              <div className="app-theme-preview-panes">
                <div className="app-theme-preview-sidebar">
                  <b /><i /><i /><i /><em /><i /><i />
                </div>
                <div className="app-theme-preview-list">
                  <b />
                  {[0, 1, 2].map((row) => (
                    <div key={row} className="app-theme-preview-row"><i /><span><b /><em /></span></div>
                  ))}
                </div>
                <div className="app-theme-preview-reader">
                  <b /><span /><hr /><i /><i /><i /><i /><em />
                </div>
              </div>
            </div>
            <div className="app-theme-option-info">
              <div className="app-theme-option-heading">
                <span className="app-theme-option-name">{t(theme.labelKey, theme.name)}</span>
              </div>
              <p id={`${id}-${theme.id}-description`}>{t(theme.descriptionKey, theme.description)}</p>
            </div>
          </button>
        );
      })}
    </div>
  );
}
