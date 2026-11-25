import { useTranslation } from "react-i18next";
import { Languages, RefreshCw, Settings, Square, X } from "lucide-react";
import { AI_LANGUAGE_OPTIONS, openAiSettings } from "@/lib/ai";
import type {
  BilingualWarning,
  TranslationDisplayMode,
  TranslationProgress,
} from "@/hooks/useBilingualTranslation";

interface Props {
  displayMode: TranslationDisplayMode;
  onDisplayModeChange: (mode: TranslationDisplayMode) => void;
  targetLang: string;
  onTargetLangChange: (lang: string) => void;
  loading: boolean;
  progress: TranslationProgress | null;
  cached?: boolean;
  model?: string;
  warning: BilingualWarning | null;
  configIssue: "unconfigured" | "disabled" | "keyring" | null;
  needsPrivacyAck: boolean;
  endpointHost: string;
  onAcceptPrivacy: () => void;
  onRetranslate: () => void;
  onStop: () => void;
  onClose: () => void;
}

export default function TranslationBar({
  displayMode,
  onDisplayModeChange,
  targetLang,
  onTargetLangChange,
  loading,
  progress,
  cached,
  model,
  warning,
  configIssue,
  needsPrivacyAck,
  endpointHost,
  onAcceptPrivacy,
  onRetranslate,
  onStop,
  onClose,
}: Props) {
  const { t } = useTranslation();

  if (configIssue) {
    const title =
      configIssue === "keyring"
        ? t("ai.keyringLocked")
        : configIssue === "disabled"
          ? t("ai.disabledTitle")
          : t("ai.unconfiguredTitle");
    const desc =
      configIssue === "disabled"
        ? t("ai.disabledDesc")
        : configIssue === "unconfigured"
          ? t("ai.unconfiguredDesc")
          : "";
    return (
      <div className="ai-inline-notice" role="status" style={{ marginBottom: "16px" }}>
        <div>
          <strong>{title}</strong>
          {desc && <div>{desc}</div>}
        </div>
        <div style={{ display: "inline-flex", alignItems: "center", gap: "6px" }}>
          <button type="button" className="ai-push-btn ai-push-btn--primary" onClick={openAiSettings}>
            <Settings size={12} />
            {t("ai.openSettings")}
          </button>
          <button
            type="button"
            className="ai-summary-icon-btn"
            onClick={onClose}
            aria-label={t("ai.close")}
            title={t("ai.close")}
          >
            <X size={14} />
          </button>
        </div>
      </div>
    );
  }

  if (needsPrivacyAck) {
    return (
      <div className="ai-inline-notice" role="status" style={{ marginBottom: "16px" }}>
        <span>
          {endpointHost
            ? t("ai.privacyNoticeHost", { host: endpointHost })
            : t("translate.privacyNotice")}
        </span>
        <div style={{ display: "inline-flex", alignItems: "center", gap: "6px" }}>
          <button type="button" className="ai-push-btn ai-push-btn--primary" onClick={onAcceptPrivacy}>
            {t("translate.acceptAndContinue")}
          </button>
          <button
            type="button"
            className="ai-summary-icon-btn"
            onClick={onClose}
            aria-label={t("ai.close")}
            title={t("ai.close")}
          >
            <X size={14} />
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="translation-bar" role="region" aria-label={t("translate.title")}>
      <Languages size={14} style={{ color: "var(--color-accent)", flexShrink: 0 }} />

      <div className="translation-bar-segmented" role="group" aria-label={t("ai.translateDisplayMode")}>
        {([
          { id: "bilingual", label: t("ai.modeBilingual") },
          { id: "translated", label: t("ai.modeTranslated") },
          { id: "original", label: t("ai.modeOriginal") },
        ] as const).map((item) => (
          <button
            key={item.id}
            type="button"
            className="translation-bar-seg-btn"
            aria-pressed={displayMode === item.id}
            onClick={() => onDisplayModeChange(item.id)}
          >
            {item.label}
          </button>
        ))}
      </div>

      <select
        className="ai-summary-select"
        aria-label={t("ai.translateDefaultLang")}
        value={targetLang}
        onChange={(e) => onTargetLangChange(e.target.value)}
      >
        {AI_LANGUAGE_OPTIONS.map((option) => (
          <option key={option.code} value={option.code}>
            {t(option.labelKey)}
          </option>
        ))}
      </select>

      {loading ? (
        <span role="status" aria-live="polite" className="translation-bar-status">
          {progress && progress.total > 0
            ? t("ai.translatingProgress", { done: progress.done, total: progress.total })
            : t("common.translating", "Translating...")}
        </span>
      ) : warning ? (
        <span role="status" className="translation-bar-status" style={{ color: "var(--color-signal-text)" }}>
          {t("common.translationIncomplete", "Some content could not be translated ({{done}}/{{total}}).", {
            done: warning.done,
            total: warning.total,
          })}
        </span>
      ) : (
        <span className="ai-summary-meta">
          {cached && <span className="ai-summary-badge">{t("ai.cachedBadge")}</span>}
          {model && <span>{model}</span>}
        </span>
      )}

      <div className="translation-bar-trailing">
        {loading ? (
          <button
            type="button"
            className="ai-push-btn"
            onClick={onStop}
            aria-label={t("ai.stop")}
            title={t("ai.stop")}
          >
            <Square size={11} />
            {t("ai.stop")}
          </button>
        ) : (
          <button
            type="button"
            className="ai-summary-icon-btn"
            onClick={onRetranslate}
            aria-label={t("ai.retranslate")}
            title={t("ai.retranslate")}
          >
            <RefreshCw size={13} />
          </button>
        )}
        <button
          type="button"
          className="ai-summary-icon-btn"
          onClick={onClose}
          aria-label={t("ai.close")}
          title={t("ai.close")}
        >
          <X size={14} />
        </button>
      </div>
    </div>
  );
}
