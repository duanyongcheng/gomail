import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, ChevronDown, ChevronRight, Copy, RefreshCw, Settings, Sparkles, Square, X } from "lucide-react";
import { AIService, events, type AIConfigView, type SummaryResult } from "@/mygo";
import {
  AI_LANGUAGE_OPTIONS,
  acceptAiPrivacy,
  classifyAiError,
  formatSummaryForClipboard,
  isAiPrivacyAccepted,
  nextAiRequestId,
  openAiSettings,
  resolveUiLanguageOption,
  type AiErrorKind,
} from "@/lib/ai";
import { useUIStore } from "@/stores/ui.store";

interface Props {
  messageId?: string;
  threadId?: string;
  onClose: () => void;
}

/**
 * Collapsible Clarity AI summary panel for single messages and conversation
 * threads. Streams incremental tokens, renders structured TL;DR / Key Points /
 * Action Items, respects local cache, and supports instant cancellation.
 */
export default function AiSummaryCard({ messageId, threadId, onClose }: Props) {
  const { t } = useTranslation();
  const uiLanguage = useUIStore((s) => s.language);
  const isThread = Boolean(threadId && !messageId);
  const targetKey = isThread ? `thread:${threadId}` : `msg:${messageId ?? ""}`;

  const [config, setConfig] = useState<AIConfigView | null>(null);
  const [configLoaded, setConfigLoaded] = useState(false);
  const [summary, setSummary] = useState<SummaryResult | null>(null);
  const [streamText, setStreamText] = useState("");
  const [loading, setLoading] = useState(false);
  const [errorKind, setErrorKind] = useState<AiErrorKind | null>(null);
  const [errorMsg, setErrorMsg] = useState("");
  const [collapsed, setCollapsed] = useState(false);
  const [copied, setCopied] = useState(false);
  const [language, setLanguage] = useState(() => resolveUiLanguageOption(uiLanguage));
  const [privacyAcked, setPrivacyAcked] = useState(false);

  const activeRequestIdRef = useRef<string | null>(null);
  const generationRef = useRef(0);

  // Load AI config once so we can present friendly unconfigured guidance and host-specific privacy notice.
  useEffect(() => {
    let active = true;
    void AIService.getConfig({})
      .then((cfg) => {
        if (!active) return;
        setConfig(cfg);
        if (cfg.summaryLanguage && AI_LANGUAGE_OPTIONS.some((o) => o.code === cfg.summaryLanguage)) {
          setLanguage(cfg.summaryLanguage);
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

  // Subscribe to backend streaming chunks.
  useEffect(() => {
    const unlisten = events?.aiStream?.on?.((ev) => {
      if (!ev || ev.requestId !== activeRequestIdRef.current) return;
      if (ev.delta) {
        setStreamText((prev) => prev + ev.delta);
      }
    });
    return () => {
      if (typeof unlisten === "function") unlisten();
    };
  }, []);

  const cancelInFlight = useCallback(() => {
    const reqId = activeRequestIdRef.current;
    if (reqId) {
      activeRequestIdRef.current = null;
      void AIService.cancel({ requestId: reqId }).catch(() => {});
    }
  }, []);

  useEffect(() => () => { cancelInFlight(); }, [cancelInFlight]);

  const run = useCallback(
    async (targetLang: string, forceRefresh = false) => {
      cancelInFlight();
      const gen = ++generationRef.current;
      const requestId = nextAiRequestId(isThread ? "sum-thread" : "sum-msg");
      activeRequestIdRef.current = requestId;
      setLoading(true);
      setErrorKind(null);
      setErrorMsg("");
      setStreamText("");
      if (forceRefresh) setSummary(null);

      try {
        const result = isThread
          ? await AIService.summarizeThread({
              requestId,
              threadId: threadId!,
              targetLang,
              style: config?.summaryStyle ?? "",
              forceRefresh,
            })
          : await AIService.summarizeMessage({
              requestId,
              messageId: messageId!,
              targetLang,
              style: config?.summaryStyle ?? "",
              forceRefresh,
            });
        if (gen !== generationRef.current) return;
        setSummary(result);
        setStreamText("");
      } catch (err) {
        if (gen !== generationRef.current) return;
        const classified = classifyAiError(err);
        if (classified.kind === "cancelled") {
          setErrorKind("cancelled");
          setErrorMsg("");
        } else {
          setErrorKind(classified.kind);
          setErrorMsg(classified.message);
        }
      } finally {
        if (activeRequestIdRef.current === requestId) {
          activeRequestIdRef.current = null;
        }
        if (gen === generationRef.current) {
          setLoading(false);
        }
      }
    },
    [cancelInFlight, config?.summaryStyle, isThread, messageId, threadId],
  );

  const canRun =
    configLoaded &&
    privacyAcked &&
    (!config || (!config.keyringError && config.configured && config.enabled));

  useEffect(() => {
    if (!canRun) return;
    if (!messageId && !threadId) return;
    setSummary(null);
    void run(language, false);
  }, [canRun, language, messageId, run, targetKey, threadId]);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(timer);
  }, [copied]);

  function handleAcceptPrivacy() {
    acceptAiPrivacy(config?.endpointHost);
    setPrivacyAcked(true);
  }

  function handleStop() {
    cancelInFlight();
    setLoading(false);
    setErrorKind("cancelled");
    setErrorMsg(t("ai.stopped"));
  }

  async function handleCopy() {
    const text = summary
      ? formatSummaryForClipboard(summary, t("ai.actionItems"))
      : streamText;
    if (!text) return;
    await navigator.clipboard.writeText(text);
    setCopied(true);
  }

  const showKeyringError = configLoaded && (config?.keyringError || errorKind === "keyring");
  const showUnconfigured = configLoaded && ((config && !config.configured && !config.keyringError) || errorKind === "unconfigured");
  const showDisabled = configLoaded && ((config && config.configured && !config.enabled) || errorKind === "disabled");

  return (
    <section className="ai-summary-card" aria-label={isThread ? t("ai.threadSummaryTitle") : t("ai.summaryTitle")}>
      <div className="ai-summary-header">
        <button
          type="button"
          className="ai-summary-icon-btn"
          onClick={() => setCollapsed((v) => !v)}
          aria-expanded={!collapsed}
          aria-label={collapsed ? t("ai.expand") : t("ai.collapse")}
          title={collapsed ? t("ai.expand") : t("ai.collapse")}
        >
          {collapsed ? <ChevronRight size={14} /> : <ChevronDown size={14} />}
        </button>
        <Sparkles size={14} style={{ color: "var(--color-accent)", flexShrink: 0 }} />
        <span className="ai-summary-title">
          {isThread ? t("ai.threadSummaryTitle") : t("ai.summaryTitle")}
        </span>
        <span className="ai-summary-meta">
          {summary?.cached && <span className="ai-summary-badge">{t("ai.cachedBadge")}</span>}
          {summary?.model && <span>{summary.model}</span>}
        </span>

        <div className="ai-summary-controls">
          <select
            className="ai-summary-select"
            aria-label={t("ai.summaryLanguage")}
            value={language}
            onChange={(e) => setLanguage(e.target.value)}
          >
            {AI_LANGUAGE_OPTIONS.map((option) => (
              <option key={option.code} value={option.code}>
                {t(option.labelKey)}
              </option>
            ))}
          </select>

          {loading ? (
            <button
              type="button"
              className="ai-push-btn"
              onClick={handleStop}
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
              onClick={() => void run(language, true)}
              disabled={!canRun}
              aria-label={t("ai.regenerate")}
              title={t("ai.regenerate")}
            >
              <RefreshCw size={14} />
            </button>
          )}

          <button
            type="button"
            className="ai-summary-icon-btn"
            onClick={() => { cancelInFlight(); onClose(); }}
            aria-label={t("ai.close")}
            title={t("ai.close")}
          >
            <X size={14} />
          </button>
        </div>
      </div>

      {!collapsed && (
        <div className="ai-summary-body">
          {showKeyringError ? (
            <div role="alert" className="ai-inline-notice">
              <span>{t("ai.keyringLocked")}</span>
              <button type="button" className="ai-push-btn ai-push-btn--primary" onClick={openAiSettings}>
                <Settings size={12} />
                {t("ai.openSettings")}
              </button>
            </div>
          ) : showUnconfigured ? (
            <div role="status" className="ai-inline-notice">
              <div>
                <strong>{t("ai.unconfiguredTitle")}</strong>
                <div>{t("ai.unconfiguredDesc")}</div>
              </div>
              <button type="button" className="ai-push-btn ai-push-btn--primary" onClick={openAiSettings}>
                <Settings size={12} />
                {t("ai.openSettings")}
              </button>
            </div>
          ) : showDisabled ? (
            <div role="status" className="ai-inline-notice">
              <div>
                <strong>{t("ai.disabledTitle")}</strong>
                <div>{t("ai.disabledDesc")}</div>
              </div>
              <button type="button" className="ai-push-btn ai-push-btn--primary" onClick={openAiSettings}>
                <Settings size={12} />
                {t("ai.openSettings")}
              </button>
            </div>
          ) : !privacyAcked && configLoaded ? (
            <div className="ai-inline-notice">
              <span>
                {config?.endpointHost
                  ? t("ai.privacyNoticeHost", { host: config.endpointHost })
                  : t("ai.privacyNotice")}
              </span>
              <button type="button" className="ai-push-btn ai-push-btn--primary" onClick={handleAcceptPrivacy}>
                {t("translate.acceptAndContinue")}
              </button>
            </div>
          ) : loading ? (
            <div>
              {streamText ? (
                <div style={{ whiteSpace: "pre-wrap", wordBreak: "break-word" }}>{streamText}</div>
              ) : (
                <div role="status" aria-live="polite" style={{ color: "var(--color-text-secondary)" }}>
                  {t("ai.summarizing")}
                </div>
              )}
            </div>
          ) : errorKind && errorKind !== "cancelled" ? (
            <div role="alert" className="ai-inline-notice">
              <div>
                <strong>{t("ai.summaryFailed")}</strong>
                <div>{errorMsg}</div>
              </div>
              <button type="button" className="ai-push-btn" onClick={() => void run(language, true)}>
                <RefreshCw size={12} />
                {t("ai.retry")}
              </button>
            </div>
          ) : errorKind === "cancelled" && !summary ? (
            <div role="status" style={{ color: "var(--color-text-secondary)", fontSize: "12.5px" }}>
              {streamText && (
                <div style={{ whiteSpace: "pre-wrap", wordBreak: "break-word", color: "var(--color-text-primary)", marginBottom: "8px" }}>
                  {streamText}
                </div>
              )}
              <span>{t("ai.stopped")}</span>
            </div>
          ) : summary ? (
            <>
              {summary.tldr && <p className="ai-summary-tldr">{summary.tldr}</p>}
              {summary.key_points && summary.key_points.length > 0 && (
                <ul className="ai-summary-points">
                  {summary.key_points.map((point, index) => (
                    <li key={index}>{point}</li>
                  ))}
                </ul>
              )}
              {!summary.tldr && (!summary.key_points || summary.key_points.length === 0) && summary.raw_text && (
                <div style={{ whiteSpace: "pre-wrap", wordBreak: "break-word" }}>{summary.raw_text}</div>
              )}
              {summary.action_items && summary.action_items.length > 0 && (
                <div className="ai-summary-actions-block">
                  <div className="ai-summary-actions-label">{t("ai.actionItems")}</div>
                  <ul className="ai-summary-action-list">
                    {summary.action_items.map((item, index) => (
                      <li key={index} className="ai-summary-action-item">
                        <span>• {item.text}</span>
                        {item.due && <span className="ai-summary-due">{item.due}</span>}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              <div className="ai-summary-footer">
                <span className="ai-summary-note">
                  {summary.truncated ? t("ai.truncatedNotice") : config?.isLocalEndpoint ? t("ai.privacyNoticeLocal", { host: config.endpointHost }) : ""}
                </span>
                <button type="button" className="ai-push-btn" onClick={() => void handleCopy()}>
                  {copied ? <Check size={12} /> : <Copy size={12} />}
                  {copied ? t("ai.copied") : t("ai.copy")}
                </button>
              </div>
            </>
          ) : null}
        </div>
      )}
    </section>
  );
}
