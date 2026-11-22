import { useEffect, useId, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { AccountService, type GoogleOAuthConfig as GoogleOAuthConfigData } from "@/mygo";
import { extractErrorMessage } from "@/lib/extractErrorMessage";
import { inputStyle } from "@/styles/form";

const buttonStyle = {
  padding: "6px 12px",
  border: "1px solid var(--color-border)",
  borderRadius: 6,
  background: "var(--color-bg)",
  color: "var(--color-text-primary)",
  fontSize: 12,
  cursor: "pointer",
};

function GoogleOAuthConfigPanel({
  onChanged,
  onBusy,
}: {
  onChanged?: (config: GoogleOAuthConfigData) => void;
  onBusy?: (busy: boolean) => void;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const idPrefix = useId();
  const [config, setConfig] = useState<GoogleOAuthConfigData | null>(null);
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [clearSecret, setClearSecret] = useState(false);
  const [ready, setReady] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);
  const [retryCount, setRetryCount] = useState(0);
  const [busyAction, setBusyAction] = useState<"save" | "clear" | null>(null);
  const [statusMessage, setStatusMessage] = useState("");
  const [error, setError] = useState("");
  const alive = useRef(true);
  const lock = useRef(false);

  useEffect(() => {
    let active = true;
    alive.current = true;
    setReady(false);
    setLoadFailed(false);
    void AccountService.googleOAuthConfig({})
      .then((value) => {
        if (!active) return;
        setConfig(value);
        setClientId(value.appClientId || value.clientId || "");
        setClientSecret("");
        setClearSecret(false);
        setReady(true);
      })
      .catch(() => {
        if (active) setLoadFailed(true);
      });
    return () => {
      active = false;
      alive.current = false;
    };
  }, [retryCount]);

  function sourceLabel(source: string) {
    switch (source) {
      case "app":
        return t("googleOAuth.sourceApp", "Active source: In-app configuration");
      case "env":
        return t("googleOAuth.sourceEnv", "Active source: Environment variable");
      default:
        return t("googleOAuth.sourceNone", "Active source: Not configured");
    }
  }

  async function invalidateQueries() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["account-connections"] }),
      queryClient.invalidateQueries({ queryKey: ["accounts"] }),
    ]);
  }

  async function handleSave(event: React.FormEvent) {
    event.preventDefault();
    if (!ready || lock.current || !clientId.trim()) return;
    lock.current = true;
    setBusyAction("save");
    onBusy?.(true);
    setError("");
    setStatusMessage("");
    try {
      const trimmedSecret = clearSecret ? "" : clientSecret.trim();
      const keepExistingSecret = !clearSecret && trimmedSecret === "" && Boolean(config?.appHasSecret);
      const updated = await AccountService.saveGoogleOAuthConfig({
        clientId: clientId.trim(),
        clientSecret: trimmedSecret,
        keepExistingSecret,
      });
      await invalidateQueries();
      if (alive.current) {
        setConfig(updated);
        setClientId(updated.appClientId || updated.clientId || "");
        setClientSecret("");
        setClearSecret(false);
        setStatusMessage(t("googleOAuth.saved", "Google OAuth configuration saved and applied."));
        onChanged?.(updated);
      }
    } catch (err) {
      if (alive.current) setError(extractErrorMessage(err));
    } finally {
      lock.current = false;
      if (alive.current) {
        setBusyAction(null);
        onBusy?.(false);
      }
    }
  }

  async function handleClear() {
    if (!ready || lock.current || !config?.hasAppConfig) return;
    lock.current = true;
    setBusyAction("clear");
    onBusy?.(true);
    setError("");
    setStatusMessage("");
    try {
      const updated = await AccountService.clearGoogleOAuthConfig({});
      await invalidateQueries();
      if (alive.current) {
        setConfig(updated);
        setClientId(updated.appClientId || updated.clientId || "");
        setClientSecret("");
        setClearSecret(false);
        setStatusMessage(t("googleOAuth.cleared", "In-app Google OAuth configuration cleared."));
        onChanged?.(updated);
      }
    } catch (err) {
      if (alive.current) setError(extractErrorMessage(err));
    } finally {
      lock.current = false;
      if (alive.current) {
        setBusyAction(null);
        onBusy?.(false);
      }
    }
  }

  const busy = busyAction !== null;
  const clientIdInputId = `${idPrefix}-client-id`;
  const clientSecretInputId = `${idPrefix}-client-secret`;
  const clearSecretId = `${idPrefix}-clear-secret`;

  return (
    <form
      aria-label={t("googleOAuth.title", "Google OAuth")}
      onSubmit={(event) => void handleSave(event)}
      style={{
        display: "grid",
        gap: 10,
        marginTop: 10,
        padding: 12,
        border: "1px solid var(--color-border)",
        borderRadius: 8,
        fontSize: 12,
      }}
    >
      <p style={{ margin: 0, color: "var(--color-text-secondary)", lineHeight: 1.5 }}>
        {t(
          "googleOAuth.notice",
          "Configure your own Google Cloud Desktop OAuth Client ID and optional Client Secret. Saved credentials are encrypted with your profile keyring and take effect immediately without restarting. Changing Client ID requires existing Gmail accounts to reconnect while keeping your local mail and drafts.",
        )}
      </p>

      {!ready && !loadFailed && (
        <p role="status" style={{ margin: 0 }}>
          {t("common.loading", "Loading...")}
        </p>
      )}

      {loadFailed && (
        <p role="alert" style={{ margin: 0 }}>
          {t("googleOAuth.loadFailed", "Could not load Google OAuth configuration.")}{" "}
          <button type="button" style={buttonStyle} onClick={() => setRetryCount((value) => value + 1)}>
            {t("common.retry", "Retry")}
          </button>
        </p>
      )}

      {ready && config && (
        <>
          <div style={{ display: "grid", gap: 4, color: "var(--color-text-secondary)" }}>
            <span>{sourceLabel(config.source)}</span>
            <span>
              {config.hasSecret
                ? t("googleOAuth.secretStatusSet", "Client secret: configured")
                : t("googleOAuth.secretStatusEmpty", "Client secret: not set")}
            </span>
          </div>

          {config.keyringError && (
            <p role="alert" style={{ margin: 0, color: "#dc3545" }}>
              {t(
                "googleOAuth.keyringLocked",
                "Saved Google OAuth configuration could not be decrypted because the profile keyring is unavailable. Unlock your keyring, or save new credentials / clear the saved override.",
              )}
            </p>
          )}

          <div style={{ display: "grid", gap: 4 }}>
            <label htmlFor={clientIdInputId} style={{ fontWeight: 500, color: "var(--color-text-primary)" }}>
              {t("googleOAuth.clientId", "Google Client ID")}
            </label>
            <input
              id={clientIdInputId}
              type="text"
              autoComplete="off"
              spellCheck={false}
              style={inputStyle}
              disabled={busy}
              value={clientId}
              placeholder={t("googleOAuth.clientIdPlaceholder", "your-desktop-client-id.apps.googleusercontent.com")}
              onChange={(event) => {
                setClientId(event.target.value);
                setStatusMessage("");
              }}
            />
          </div>

          <div style={{ display: "grid", gap: 4 }}>
            <label htmlFor={clientSecretInputId} style={{ fontWeight: 500, color: "var(--color-text-primary)" }}>
              {t("googleOAuth.clientSecret", "Google Client Secret (optional)")}
            </label>
            <input
              id={clientSecretInputId}
              type="password"
              autoComplete="off"
              style={inputStyle}
              disabled={busy || clearSecret}
              value={clientSecret}
              placeholder={
                config.appHasSecret
                  ? t("googleOAuth.secretSavedPlaceholder", "Saved (leave blank to keep existing secret)")
                  : t("googleOAuth.secretPlaceholder", "Optional desktop client secret")
              }
              onChange={(event) => {
                setClientSecret(event.target.value);
                setStatusMessage("");
              }}
            />
          </div>

          {config.appHasSecret && (
            <label htmlFor={clearSecretId} style={{ display: "flex", alignItems: "center", gap: 6, color: "var(--color-text-secondary)" }}>
              <input
                id={clearSecretId}
                type="checkbox"
                disabled={busy}
                checked={clearSecret}
                onChange={(event) => {
                  const checked = event.target.checked;
                  setClearSecret(checked);
                  if (checked) setClientSecret("");
                  setStatusMessage("");
                }}
              />
              {t("googleOAuth.clearSecret", "Clear saved secret")}
            </label>
          )}

          <div style={{ display: "flex", flexWrap: "wrap", gap: 8, marginTop: 4 }}>
            <button type="submit" style={buttonStyle} disabled={busy || !clientId.trim()}>
              {busyAction === "save"
                ? t("googleOAuth.saving", "Saving...")
                : t("googleOAuth.save", "Save Google OAuth")}
            </button>
            {config.hasAppConfig && (
              <button type="button" style={buttonStyle} disabled={busy} onClick={() => void handleClear()}>
                {busyAction === "clear"
                  ? t("googleOAuth.clearing", "Clearing...")
                  : t("googleOAuth.clear", "Clear in-app configuration")}
              </button>
            )}
          </div>
        </>
      )}

      {statusMessage && (
        <p role="status" style={{ margin: 0, color: "#15803d" }}>
          {statusMessage}
        </p>
      )}
      {error && (
        <p role="alert" style={{ margin: 0, color: "#dc3545" }}>
          {error}
        </p>
      )}
    </form>
  );
}

export default function GoogleOAuthConfig({
  onChanged,
  onBusy,
  defaultOpen = false,
}: {
  onChanged?: (config: GoogleOAuthConfigData) => void;
  onBusy?: (busy: boolean) => void;
  defaultOpen?: boolean;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(defaultOpen);

  return (
    <div style={{ marginTop: 16 }}>
      <button type="button" style={buttonStyle} aria-expanded={open} onClick={() => setOpen((value) => !value)}>
        {t("googleOAuth.toggle", "Configure Google OAuth")}
      </button>
      {open && <GoogleOAuthConfigPanel onChanged={onChanged} onBusy={onBusy} />}
    </div>
  );
}
