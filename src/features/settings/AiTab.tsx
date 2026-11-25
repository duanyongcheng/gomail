import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { AIService, type AIConfigView } from "@/mygo";
import { AI_LANGUAGE_OPTIONS, AI_PRESETS, isLoopbackEndpoint, isRemoteHttpEndpoint } from "@/lib/ai";
import { extractErrorMessage } from "@/lib/extractErrorMessage";
import { useToastStore } from "@/stores/toast.store";
import ModelPicker from "./ModelPicker";

const fieldStyle: React.CSSProperties = {
  marginBottom: "12px",
};

const labelStyle: React.CSSProperties = {
  display: "block",
  fontSize: "12px",
  fontWeight: 500,
  color: "var(--color-text-secondary)",
  marginBottom: "4px",
};

const inputStyle: React.CSSProperties = {
  width: "100%",
  padding: "7px 10px",
  fontSize: "13px",
  boxSizing: "border-box",
};

const gridTwoCol: React.CSSProperties = {
  display: "grid",
  gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))",
  gap: "12px",
};

/**
 * Unified AI & Translation settings (Clarity inset-group grammar).
 *
 * One encrypted provider config drives both AI email summaries (single message
 * and thread) and AI translation (bilingual reading, translated-only, subject,
 * and selection popover), with optional task-level model overrides.
 */
export default function AiTab() {
  const { t } = useTranslation();
  const [loaded, setLoaded] = useState<AIConfigView | null>(null);
  const [enabled, setEnabled] = useState(false);
  const [preset, setPreset] = useState("openai");
  const [provider, setProvider] = useState<"openai_compatible" | "anthropic">("openai_compatible");
  const [endpoint, setEndpoint] = useState("https://api.openai.com/v1");
  const [apiKey, setApiKey] = useState("");
  const [clearSavedKey, setClearSavedKey] = useState(false);
  const [model, setModel] = useState("gpt-4o-mini");
  const [apiMode, setApiMode] = useState<"completions" | "responses">("completions");
  const [proxyMode, setProxyMode] = useState<"inherit" | "disabled">("inherit");
  const [allowInsecureHttp, setAllowInsecureHttp] = useState(false);

  const [summaryLanguage, setSummaryLanguage] = useState("");
  const [summaryStyle, setSummaryStyle] = useState<"standard" | "concise">("standard");
  const [summaryModel, setSummaryModel] = useState("");

  const [translateLanguage, setTranslateLanguage] = useState("");
  const [translateDisplayMode, setTranslateDisplayMode] = useState<"bilingual" | "translated">("bilingual");
  const [translateModel, setTranslateModel] = useState("");

  const [statusMsg, setStatusMsg] = useState("");
  const [statusType, setStatusType] = useState<"success" | "error" | "">("");
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [clearingCache, setClearingCache] = useState(false);

  function applyConfigView(cfg: AIConfigView) {
    setLoaded(cfg);
    setEnabled(cfg.enabled);
    setPreset(cfg.preset || "openai");
    setProvider(cfg.provider === "anthropic" ? "anthropic" : "openai_compatible");
    setEndpoint(cfg.endpoint || (cfg.configured ? "" : "https://api.openai.com/v1"));
    setApiKey("");
    setClearSavedKey(false);
    setModel(cfg.model || "");
    setApiMode(cfg.apiMode === "responses" ? "responses" : "completions");
    setProxyMode(cfg.proxyMode === "disabled" ? "disabled" : "inherit");
    setAllowInsecureHttp(Boolean(cfg.allowInsecureHttp));
    setSummaryLanguage(cfg.summaryLanguage || "");
    setSummaryStyle(cfg.summaryStyle === "concise" ? "concise" : "standard");
    setSummaryModel(cfg.summaryModel || "");
    setTranslateLanguage(cfg.translateLanguage || "");
    setTranslateDisplayMode(cfg.translateDisplayMode === "translated" ? "translated" : "bilingual");
    setTranslateModel(cfg.translateModel || "");
  }

  useEffect(() => {
    let active = true;
    void AIService.getConfig({})
      .then((cfg) => {
        if (active && cfg) applyConfigView(cfg);
      })
      .catch((err) => {
        console.error("Failed to load AI config:", err);
      });
    return () => { active = false; };
  }, []);

  function handleSelectPreset(presetId: string) {
    setPreset(presetId);
    const chosen = AI_PRESETS.find((item) => item.id === presetId);
    if (!chosen) return;
    setProvider(chosen.provider);
    if (chosen.endpoint) setEndpoint(chosen.endpoint);
    if (chosen.defaultModel) setModel(chosen.defaultModel);
    setApiMode(chosen.apiMode);
    if (chosen.local) {
      setProxyMode("disabled");
    }
  }

  function keepExistingKey(): boolean {
    return !clearSavedKey && apiKey.trim() === "" && Boolean(loaded?.hasApiKey);
  }

  function buildTestPayload() {
    return {
      provider,
      endpoint: endpoint.trim(),
      apiKey: clearSavedKey ? "" : apiKey.trim(),
      keepExistingKey: keepExistingKey(),
      model: model.trim(),
      apiMode,
      proxyMode,
      allowInsecureHttp,
    };
  }

  async function handleSave() {
    setSaving(true);
    setStatusMsg("");
    try {
      const updated = await AIService.saveConfig({
        enabled,
        provider,
        preset,
        endpoint: endpoint.trim(),
        apiKey: clearSavedKey ? "" : apiKey.trim(),
        keepExistingKey: keepExistingKey(),
        model: model.trim(),
        apiMode,
        proxyMode,
        allowInsecureHttp,
        summaryLanguage,
        summaryStyle,
        summaryModel: summaryModel.trim(),
        translateLanguage,
        translateDisplayMode,
        translateModel: translateModel.trim(),
      });
      applyConfigView(updated);
      setStatusMsg(t("ai.configSaved"));
      setStatusType("success");
    } catch (err: unknown) {
      const errMsg = extractErrorMessage(err);
      setStatusMsg(t("ai.saveFailed", { error: errMsg }));
      setStatusType("error");
      useToastStore.getState().addToast({ message: t("ai.saveFailed", { error: errMsg }), type: "error" });
    } finally {
      setSaving(false);
    }
  }

  async function handleTest() {
    setTesting(true);
    setStatusMsg("");
    try {
      const result = await AIService.testConnection(buildTestPayload());
      setStatusMsg(t("ai.connectionOk", { result: result.message || `${result.model} (${result.latencyMs} ms)` }));
      setStatusType("success");
    } catch (err: unknown) {
      const errMsg = extractErrorMessage(err);
      setStatusMsg(t("ai.testFailed", { error: errMsg }));
      setStatusType("error");
      useToastStore.getState().addToast({ message: t("ai.testFailed", { error: errMsg }), type: "error" });
    } finally {
      setTesting(false);
    }
  }

  async function handleClearConfig() {
    setClearing(true);
    setStatusMsg("");
    try {
      const cleared = await AIService.clearConfig({});
      applyConfigView(cleared);
      setStatusMsg(t("ai.configCleared", "AI configuration cleared."));
      setStatusType("success");
    } catch (err: unknown) {
      const errMsg = extractErrorMessage(err);
      setStatusMsg(t("ai.saveFailed", { error: errMsg }));
      setStatusType("error");
    } finally {
      setClearing(false);
    }
  }

  async function handleClearCache() {
    setClearingCache(true);
    setStatusMsg("");
    try {
      const res = await AIService.clearCache({});
      const refreshed = await AIService.getConfig({});
      setLoaded(refreshed);
      setStatusMsg(t("ai.cacheCleared", { count: res.deleted }));
      setStatusType("success");
    } catch (err: unknown) {
      const errMsg = extractErrorMessage(err);
      setStatusMsg(errMsg);
      setStatusType("error");
    } finally {
      setClearingCache(false);
    }
  }

  const isLocal = isLoopbackEndpoint(endpoint);
  const showInsecureOption = allowInsecureHttp || isRemoteHttpEndpoint(endpoint);

  return (
    <div>
      <p className="settings-section-desc" style={{ marginBottom: "16px" }}>
        {t("ai.unifiedDesc")}
      </p>

      {loaded?.keyringError && (
        <div role="alert" className="settings-callout settings-callout--signal" style={{ marginBottom: "16px" }}>
          {t("ai.keyringLocked")}
        </div>
      )}

      {/* 1. Model Service */}
      <fieldset style={{ marginBottom: "16px" }}>
        <legend>{t("ai.serviceSection")}</legend>

        <label htmlFor="ai-enabled" className="settings-checkbox-row" style={{ marginBottom: "14px" }}>
          <input
            id="ai-enabled"
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
          />
          <span>{t("ai.enableAll")}</span>
        </label>

        <div style={fieldStyle}>
          <span style={labelStyle}>{t("ai.preset")}</span>
          <div className="settings-choice-grid" style={{ gap: "6px" }}>
            {AI_PRESETS.map((item) => (
              <button
                key={item.id}
                type="button"
                className={`settings-button${preset === item.id ? " settings-button--primary" : ""}`}
                aria-pressed={preset === item.id}
                onClick={() => handleSelectPreset(item.id)}
              >
                {t(item.labelKey)}
              </button>
            ))}
          </div>
        </div>

        <div style={gridTwoCol}>
          <div style={fieldStyle}>
            <label htmlFor="ai-provider" style={labelStyle}>{t("translate.provider")}</label>
            <select
              id="ai-provider"
              name="ai_provider"
              style={inputStyle}
              value={provider}
              onChange={(e) => setProvider(e.target.value as "openai_compatible" | "anthropic")}
            >
              <option value="openai_compatible">{t("ai.providerOpenAi")}</option>
              <option value="anthropic">{t("ai.providerAnthropic")}</option>
            </select>
          </div>

          {provider === "openai_compatible" && (
            <div style={fieldStyle}>
              <label htmlFor="ai-mode" style={labelStyle}>{t("translate.mode")}</label>
              <select
                id="ai-mode"
                name="ai_mode"
                style={inputStyle}
                value={apiMode}
                onChange={(e) => setApiMode(e.target.value as "completions" | "responses")}
              >
                <option value="completions">{t("translate.modeCompletions")}</option>
                <option value="responses">{t("translate.modeResponses")}</option>
              </select>
            </div>
          )}
        </div>

        <div style={fieldStyle}>
          <label htmlFor="ai-endpoint" style={labelStyle}>{t("ai.endpoint")}</label>
          <input
            id="ai-endpoint"
            name="ai_endpoint"
            type="url"
            style={inputStyle}
            value={endpoint}
            onChange={(e) => setEndpoint(e.target.value)}
            placeholder="https://api.openai.com/v1"
            autoComplete="off"
          />
          {isLocal && (
            <p className="settings-section-desc" style={{ marginTop: "6px", marginBottom: 0 }}>
              {t("ai.localDirectHint")}
            </p>
          )}
          {showInsecureOption && (
            <label htmlFor="ai-allow-http" className="settings-checkbox-row" style={{ marginTop: "8px", fontSize: "12px" }}>
              <input
                id="ai-allow-http"
                type="checkbox"
                checked={allowInsecureHttp}
                onChange={(e) => setAllowInsecureHttp(e.target.checked)}
              />
              <span>{t("ai.allowInsecureHttp")}</span>
            </label>
          )}
        </div>

        <div style={fieldStyle}>
          <label htmlFor="ai-api-key" style={labelStyle}>{t("translate.apiKey")}</label>
          <input
            id="ai-api-key"
            name="ai_api_key"
            type="password"
            style={inputStyle}
            value={apiKey}
            onChange={(e) => {
              setApiKey(e.target.value);
              if (e.target.value) setClearSavedKey(false);
            }}
            placeholder={
              loaded?.hasApiKey && !clearSavedKey
                ? t("ai.apiKeySavedPlaceholder", { masked: loaded.maskedApiKey || "••••" })
                : t("ai.apiKeyPlaceholder")
            }
            autoComplete="current-password"
          />
          {loaded?.hasApiKey && (
            <label htmlFor="ai-clear-key" className="settings-checkbox-row" style={{ marginTop: "6px", fontSize: "12px" }}>
              <input
                id="ai-clear-key"
                type="checkbox"
                checked={clearSavedKey}
                onChange={(e) => {
                  setClearSavedKey(e.target.checked);
                  if (e.target.checked) setApiKey("");
                }}
              />
              <span>{t("ai.clearApiKey")}</span>
            </label>
          )}
        </div>

        <div style={fieldStyle}>
          <ModelPicker
            id="ai-model"
            name="ai_model"
            label={t("translate.model")}
            value={model}
            onChange={setModel}
            fetchModels={() => AIService.listModels(buildTestPayload())}
            cacheKey={`${provider}:${endpoint}`}
            placeholder="gpt-4o-mini"
          />
        </div>

        <div style={{ marginBottom: 0 }}>
          <label htmlFor="ai-proxy-mode" style={labelStyle}>{t("ai.proxyMode")}</label>
          <select
            id="ai-proxy-mode"
            name="ai_proxy_mode"
            style={inputStyle}
            value={proxyMode}
            onChange={(e) => setProxyMode(e.target.value as "inherit" | "disabled")}
          >
            <option value="inherit">{t("ai.proxyInherit")}</option>
            <option value="disabled">{t("ai.proxyDisabled")}</option>
          </select>
        </div>
      </fieldset>

      {/* 2. Summary Preferences */}
      <fieldset style={{ marginBottom: "16px" }}>
        <legend>{t("ai.summarySection")}</legend>
        <div style={gridTwoCol}>
          <div style={fieldStyle}>
            <label htmlFor="ai-summary-lang" style={labelStyle}>{t("ai.summaryDefaultLang")}</label>
            <select
              id="ai-summary-lang"
              style={inputStyle}
              value={summaryLanguage}
              onChange={(e) => setSummaryLanguage(e.target.value)}
            >
              <option value="">{t("ai.followUiLang")}</option>
              {AI_LANGUAGE_OPTIONS.map((opt) => (
                <option key={opt.code} value={opt.code}>{t(opt.labelKey)}</option>
              ))}
            </select>
          </div>
          <div style={fieldStyle}>
            <label htmlFor="ai-summary-style" style={labelStyle}>{t("ai.summaryStyle")}</label>
            <select
              id="ai-summary-style"
              style={inputStyle}
              value={summaryStyle}
              onChange={(e) => setSummaryStyle(e.target.value as "standard" | "concise")}
            >
              <option value="standard">{t("ai.summaryStyleStandard")}</option>
              <option value="concise">{t("ai.summaryStyleConcise")}</option>
            </select>
          </div>
        </div>
        <div>
          <label htmlFor="ai-summary-model" style={labelStyle}>{t("ai.summaryModelOverride")}</label>
          <input
            id="ai-summary-model"
            type="text"
            style={inputStyle}
            value={summaryModel}
            onChange={(e) => setSummaryModel(e.target.value)}
            placeholder={model || "gpt-4o-mini"}
            autoComplete="off"
          />
        </div>
      </fieldset>

      {/* 3. Translation Preferences */}
      <fieldset style={{ marginBottom: "16px" }}>
        <legend>{t("ai.translateSection")}</legend>
        <div style={gridTwoCol}>
          <div style={fieldStyle}>
            <label htmlFor="ai-translate-lang" style={labelStyle}>{t("ai.translateDefaultLang")}</label>
            <select
              id="ai-translate-lang"
              style={inputStyle}
              value={translateLanguage}
              onChange={(e) => setTranslateLanguage(e.target.value)}
            >
              <option value="">{t("ai.followUiLang")}</option>
              {AI_LANGUAGE_OPTIONS.map((opt) => (
                <option key={opt.code} value={opt.code}>{t(opt.labelKey)}</option>
              ))}
            </select>
          </div>
          <div style={fieldStyle}>
            <label htmlFor="ai-translate-mode" style={labelStyle}>{t("ai.translateDisplayMode")}</label>
            <select
              id="ai-translate-mode"
              style={inputStyle}
              value={translateDisplayMode}
              onChange={(e) => setTranslateDisplayMode(e.target.value as "bilingual" | "translated")}
            >
              <option value="bilingual">{t("ai.modeBilingual")}</option>
              <option value="translated">{t("ai.modeTranslated")}</option>
            </select>
          </div>
        </div>
        <div>
          <label htmlFor="ai-translate-model" style={labelStyle}>{t("ai.translateModelOverride")}</label>
          <input
            id="ai-translate-model"
            type="text"
            style={inputStyle}
            value={translateModel}
            onChange={(e) => setTranslateModel(e.target.value)}
            placeholder={model || "gpt-4o-mini"}
            autoComplete="off"
          />
        </div>
      </fieldset>

      {/* Action buttons */}
      <div style={{ display: "flex", flexWrap: "wrap", gap: "8px", marginBottom: "16px" }}>
        <button
          type="button"
          className="settings-button settings-button--primary"
          onClick={() => void handleSave()}
          disabled={saving}
        >
          {saving ? t("common.saving") : t("common.save")}
        </button>
        <button
          type="button"
          className="settings-button"
          onClick={() => void handleTest()}
          disabled={testing}
        >
          {testing ? t("common.testing") : t("translate.testConnection")}
        </button>
        {loaded?.configured && (
          <button
            type="button"
            className="settings-button"
            onClick={() => void handleClearConfig()}
            disabled={clearing}
          >
            {t("ai.clearConfig")}
          </button>
        )}
      </div>

      {statusMsg && (
        <div
          role={statusType === "error" ? "alert" : "status"}
          aria-live="polite"
          className={`settings-callout${statusType === "error" ? " settings-callout--signal" : ""}`}
          style={{ marginBottom: "16px" }}
        >
          {statusMsg}
        </div>
      )}

      {/* 4. Local Cache */}
      <fieldset>
        <legend>{t("ai.cacheSection")}</legend>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", flexWrap: "wrap", gap: "10px" }}>
          <span style={{ fontSize: "12.5px", color: "var(--color-text-secondary)" }}>
            {t("ai.cacheStats", {
              summaries: loaded?.summaryCacheCount ?? 0,
              translations: loaded?.translateCacheCount ?? 0,
            })}
          </span>
          <button
            type="button"
            className="settings-button"
            onClick={() => void handleClearCache()}
            disabled={clearingCache || ((loaded?.summaryCacheCount ?? 0) + (loaded?.translateCacheCount ?? 0) === 0)}
          >
            {t("ai.clearCache")}
          </button>
        </div>
      </fieldset>
    </div>
  );
}
