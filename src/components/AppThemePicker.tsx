import { useId, useRef, type CSSProperties, type KeyboardEvent } from "react";
import { Check } from "lucide-react";
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
  const options = useRef<Array<HTMLButtonElement | null>>([]);

  function handleKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    let next: number;
    switch (event.key) {
      case "ArrowRight":
      case "ArrowDown":
        next = (index + 1) % APP_THEMES.length;
        break;
      case "ArrowLeft":
      case "ArrowUp":
        next = (index + APP_THEMES.length - 1) % APP_THEMES.length;
        break;
      case "Home":
        next = 0;
        break;
      case "End":
        next = APP_THEMES.length - 1;
        break;
      default:
        return;
    }
    event.preventDefault();
    onSelect(APP_THEMES[next].id);
    options.current[next]?.focus();
  }

  return (
    <div
      className="app-theme-picker"
      role="radiogroup"
      aria-label={t("appThemes.title", "Interface theme")}
    >
      {APP_THEMES.map((theme, index) => {
        const selected = activeTheme === theme.id;
        return (
          <button
            key={theme.id}
            ref={(element) => { options.current[index] = element; }}
            type="button"
            role="radio"
            aria-checked={selected}
            aria-label={`${theme.letter} ${t(theme.labelKey, theme.name)}`}
            aria-describedby={`${id}-${theme.id}-description`}
            tabIndex={selected ? 0 : -1}
            className="app-theme-option"
            data-app-theme-option={theme.id}
            onClick={() => onSelect(theme.id)}
            onKeyDown={(event) => handleKeyDown(event, index)}
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
                <span className="app-theme-option-letter">{theme.letter}</span>
                <span className="app-theme-option-name">{t(theme.labelKey, theme.name)}</span>
                <span className="app-theme-option-check" aria-hidden="true">{selected && <Check size={13} strokeWidth={2.5} />}</span>
              </div>
              <p id={`${id}-${theme.id}-description`}>{t(theme.descriptionKey, theme.description)}</p>
              <span className="app-theme-option-status">{selected ? t("appThemes.selected", "Selected") : theme.name}</span>
            </div>
          </button>
        );
      })}
    </div>
  );
}
