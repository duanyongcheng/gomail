import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, Copy, Settings, X } from "lucide-react";
import { AIService, type AIConfigView } from "@/mygo";
import { profileLocalStorage } from "@/lib/profileStorage";
import { useUIStore } from "@/stores/ui.store";
import {
  AI_LANGUAGE_OPTIONS,
  acceptAiPrivacy,
  classifyAiError,
  isAiPrivacyAccepted,
  nextAiRequestId,
  openAiSettings,
  resolveUiLanguageOption,
  type AiErrorKind,
} from "@/lib/ai";

interface Props {
  text: string;
  position: { x: number; y: number };
  onClose: () => void;
}

const TARGET_LANG_STORAGE_KEY = "gomail-translate-target-lang";

function readSavedTargetLang(): string | null {
  const saved = profileLocalStorage.getItem(TARGET_LANG_STORAGE_KEY);
  return saved && saved.trim() ? saved.trim() : null;
}

export default function TranslatePopover({ text, position, onClose }: Props) {
  const { t } = useTranslation();
  const interfaceLanguage = useUIStore((state) => state.language);
  const [config, setConfig] = useState<AIConfigView | null>(null);
  const [configLoaded, setConfigLoaded] = useState(false);
  const [translated, setTranslated] = useState("");
  const [resolvedLang, setResolvedLang] = useState("");
  const [loading, setLoading] = useState(true);
  const [errorKind, setErrorKind] = useState<AiErrorKind | null>(null);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);
  const [manualLangSelected, setManualLangSelected] = useState(false);
  const [targetLang, setTargetLang] = useState(
    () => readSavedTargetLang() ?? resolveUiLanguageOption(interfaceLanguage),
  );
  const [privacyAcked, setPrivacyAcked] = useState(false);

  const activeReqRef = useRef<string | null>(null);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  useEffect(() => {
    let active = true;
    void AIService.getConfig({})
      .then((cfg) => {
        if (!active) return;
        setConfig(cfg);
        if (!readSavedTargetLang() && cfg.translateLanguage && AI_LANGUAGE_OPTIONS.some((o) => o.code === cfg.translateLanguage)) {
          setTargetLang(cfg.translateLanguage);
        }
        setPrivacyAcked(isAiPrivacyAccepted(cfg.endpointHost, cfg.isLocalEndpoint));
        setConfigLoaded(true);
      })
      .catch(() => {
        if (!active) return;
        setPrivacyAcked(isAiPrivacyAccepted());
        setConfigLoaded(true);
      });
    return () => { active = false; };
  }, []);

  useEffect(() => {
    if (!configLoaded) return;
    if (config && (config.keyringError || !config.configured || !config.enabled)) {
      setLoading(false);
      return;
    }
    if (!privacyAcked) {
      setLoading(false);
      return;
    }

    let cancelled = false;
    const requestId = nextAiRequestId("sel-tr");
    activeReqRef.current = requestId;
    setLoading(true);
    setError("");
    setErrorKind(null);
    setTranslated("");
    setResolvedLang("");

    AIService.translateText({
      requestId,
      text,
      fromLang: "auto",
      toLang: targetLang,
      autoFlip: !manualLangSelected,
    })
      .then((result) => {
        if (cancelled) return;
        setTranslated(result.translated);
        setResolvedLang(result.targetLang || targetLang);
      })
      .catch((err) => {
        if (cancelled) return;
        const classified = classifyAiError(err);
        if (classified.kind !== "cancelled") {
          setErrorKind(classified.kind);
          setError(classified.message);
        }
      })
      .finally(() => {
        if (activeReqRef.current === requestId) activeReqRef.current = null;
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
      void AIService.cancel({ requestId }).catch(() => {});
    };
  }, [config, configLoaded, manualLangSelected, privacyAcked, targetLang, text]);

  function handleTargetLangChange(next: string) {
    setManualLangSelected(true);
    setTargetLang(next);
    profileLocalStorage.setItem(TARGET_LANG_STORAGE_KEY, next);
  }

  function handleAcceptPrivacy() {
    acceptAiPrivacy(config?.endpointHost);
    setPrivacyAcked(true);
  }

  function handleCopy() {
    void navigator.clipboard.writeText(translated);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }

  const left = Math.max(12, Math.min(position.x, window.innerWidth - 356));
  const top = Math.max(12, Math.min(position.y + 10, window.innerHeight - 240));

  const showKeyring = configLoaded && (config?.keyringError || errorKind === "keyring");
  const showUnconfigured = configLoaded && ((config && !config.configured && !config.keyringError) || errorKind === "unconfigured");
  const showDisabled = configLoaded && ((config && config.configured && !config.enabled) || errorKind === "disabled");

  const flippedLangOpt =
    resolvedLang && resolvedLang !== targetLang
      ? AI_LANGUAGE_OPTIONS.find((o) => o.code === resolvedLang)
      : null;

  return (
    <div
      className="translate-popover"
      data-gomail-overlay="true"
      role="dialog"
      aria-label={t("translate.title")}
      style={{ left, top }}
      onClick={(e) => e.stopPropagation()}
    >
      <div className="translate-popover-header">
        <span className="translate-popover-title">{t("translate.title")}</span>
        <select
          className="ai-summary-select"
          aria-label={t("ai.translateDefaultLang")}
          value={targetLang}
          onChange={(e) => handleTargetLangChange(e.target.value)}
        >
          {AI_LANGUAGE_OPTIONS.map((option) => (
            <option key={option.code} value={option.code}>
              {t(option.labelKey)}
            </option>
          ))}
        </select>
        <button
          type="button"
          className="ai-summary-icon-btn"
          onClick={onClose}
          aria-label={t("common.close", "Close")}
        >
          <X size={14} />
        </button>
      </div>

      {showKeyring || showUnconfigured || showDisabled ? (
        <div className="ai-inline-notice">
          <div>
            <strong>
              {showKeyring
                ? t("ai.keyringLocked")
                : showDisabled
                  ? t("ai.disabledTitle")
                  : t("ai.unconfiguredTitle")}
            </strong>
            {!showKeyring && (
              <div>{showDisabled ? t("ai.disabledDesc") : t("ai.unconfiguredDesc")}</div>
            )}
          </div>
          <button
            type="button"
            className="ai-push-btn ai-push-btn--primary"
            onClick={() => {
              onClose();
              openAiSettings();
            }}
          >
            <Settings size={12} />
            {t("ai.openSettings")}
          </button>
        </div>
      ) : !privacyAcked && configLoaded ? (
        <div className="ai-inline-notice">
          <span>
            {config?.endpointHost
              ? t("ai.privacyNoticeHost", { host: config.endpointHost })
              : t("translate.privacyNotice")}
          </span>
          <button type="button" className="ai-push-btn ai-push-btn--primary" onClick={handleAcceptPrivacy}>
            {t("translate.acceptAndContinue")}
          </button>
        </div>
      ) : loading ? (
        <div role="status" aria-live="polite" style={{ fontSize: "13px", color: "var(--color-text-secondary)", padding: "8px 0" }}>
          {t("common.translating")}
        </div>
      ) : error ? (
        <div role="alert" className="ai-inline-notice">
          <span>{error}</span>
        </div>
      ) : (
        <div>
          <div className="translate-popover-body">{translated}</div>
          <div className="translate-popover-footer">
            <span className="ai-summary-note">
              {flippedLangOpt ? t("ai.autoFlippedHint", { lang: t(flippedLangOpt.labelKey) }) : ""}
            </span>
            <button type="button" className="ai-push-btn" onClick={handleCopy}>
              {copied ? <Check size={12} /> : <Copy size={12} />}
              {copied ? t("common.copied") : t("common.copy")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
