import { useEffect, useRef, useState } from "react";
import { AccountService } from "@/mygo";
import { useConfirmStore } from "@/stores/confirm.store";
import { X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { startSync, completeOAuthFlow } from "@/lib/api";
import { accountsQueryKey } from "@/hooks/queries";
import { extractErrorMessage } from "@/lib/extractErrorMessage";
import { isFeatureAvailable, unavailableReason } from "@/lib/availability";
import { realtimePreferenceToPollInterval, useUIStore } from "@/stores/ui.store";
import { inputStyle, labelStyle } from "../styles/form";
import ProtocolAccountForm from "@/features/settings/ProtocolAccountForm";
import MicrosoftAccountForm from "@/features/settings/MicrosoftAccountForm";
import GoogleOAuthConfig from "@/features/settings/GoogleOAuthConfig";

const FOLDER_REFRESH_ATTEMPTS = 5;
const FOLDER_REFRESH_INTERVAL_MS = 2000;

function refreshFoldersAfterSyncStart(queryClient: QueryClient, accountId: string) {
  void queryClient.invalidateQueries({ queryKey: ["folders", accountId] });
  void queryClient.invalidateQueries({ queryKey: ["folders"] });
  const pollFolders = (attempts: number) => {
    if (attempts <= 0) return;
    window.setTimeout(() => {
      void queryClient.invalidateQueries({ queryKey: ["folders"] });
      void queryClient.invalidateQueries({ queryKey: ["folders", accountId] });
      pollFolders(attempts - 1);
    }, FOLDER_REFRESH_INTERVAL_MS);
  };
  pollFolders(FOLDER_REFRESH_ATTEMPTS);
}

export default function AccountSetup({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const realtimeMode = useUIStore((state) => state.realtimeMode);
  const [accountLabel, setAccountLabel] = useState("");
  const [provider, setProvider] = useState("gmail");
  const [loading, setLoading] = useState(false);
  const busy = useRef(false);
  const [error, setError] = useState<string | null>(null);
  const [configured, setConfigured] = useState(false);
  const [statusError, setStatusError] = useState(false);
  const [statusLoaded, setStatusLoaded] = useState(false);
  const [retry, setRetry] = useState(0);
  const dialogRef = useRef<HTMLDivElement>(null);
  const labelRef = useRef<HTMLInputElement>(null);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; };
  }, []);
  useEffect(() => {
    let active = true;
    setStatusLoaded(false);
    setStatusError(false);
    void AccountService.status({}).then((status) => {
      if (active) { setConfigured(status.configured); setStatusLoaded(true); }
    }).catch(() => {
      if (active) { setConfigured(false); setStatusError(true); }
    });
    return () => { active = false; };
  }, [retry]);

  async function requestClose() {
    if (busy.current) return;
    if ((accountLabel || provider !== "gmail") && !await useConfirmStore.getState().confirm({
      title: t("accountSetup.discardTitle", "Discard form"),
      message: t("accountSetup.discardConfirm", "Discard this form?"),
      destructive: true,
    })) return;
    onClose();
  }
  const closeRef = useRef(requestClose);
  closeRef.current = requestClose;
  useEffect(() => {
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    labelRef.current?.focus();
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault();
        void closeRef.current();
      }
      if (event.key === "Tab" && dialogRef.current) {
        const focusable = dialogRef.current.querySelectorAll<HTMLElement>(
          'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex="0"]',
        );
        if (!focusable.length) { event.preventDefault(); return; }
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault(); last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault(); first.focus();
        }
      }
    }
    document.addEventListener("keydown", handleKeyDown);
    return () => { document.removeEventListener("keydown", handleKeyDown); previousFocus?.focus(); };
  }, []);

  async function handleOAuth() {
    if (busy.current || !statusLoaded || !configured) return;
    busy.current = true;
    setLoading(true);
    setError(null);
    try {
      // Gmail determines the mailbox identity. No unsupported proxy/transport fields.
      const account = await completeOAuthFlow("gmail", "", "", undefined, undefined, accountLabel || undefined);
      await queryClient.invalidateQueries({ queryKey: accountsQueryKey });
      await startSync(account.id, realtimePreferenceToPollInterval(realtimeMode));
      refreshFoldersAfterSyncStart(queryClient, account.id);
      if (alive.current) onClose();
    } catch (err) {
      if (alive.current) setError(extractErrorMessage(err));
    } finally {
      busy.current = false;
      if (alive.current) setLoading(false);
    }
  }

  return (
    <div
      className="overlay-backdrop overlay-backdrop--dialog"
      role="dialog"
      aria-modal="true"
      aria-labelledby="account-setup-title"
      aria-busy={loading}
    >
      <div
        ref={dialogRef}
        style={{
          width: "min(480px, calc(100vw - 32px))",
          backgroundColor: "var(--color-bg)",
          border: "1px solid var(--color-border-strong)",
          borderRadius: "var(--ui-radius-lg)",
          boxShadow: "var(--ui-shadow-lg)",
          display: "flex",
          flexDirection: "column",
          maxHeight: "90vh",
          overflow: "hidden",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", flexShrink: 0, padding: "16px 20px", borderBottom: "1px solid var(--color-border)" }}>
          <h2 id="account-setup-title" style={{ margin: 0, fontSize: 16, fontWeight: 650, color: "var(--color-text-primary)" }}>{t("accountSetup.title", "Add Email Account")}</h2>
          <button
            type="button"
            disabled={loading}
            onClick={() => void requestClose()}
            aria-label={t("common.close", "Close")}
            style={{
              width: 30,
              height: 30,
              display: "inline-flex",
              alignItems: "center",
              justifyContent: "center",
              borderRadius: "var(--ui-radius-sm)",
              border: "1px solid transparent",
              background: "transparent",
              color: "var(--color-text-secondary)",
            }}
          >
            <X size={16} />
          </button>
        </div>
        <div className="scroll-region account-setup-scroll" style={{ flex: "1 1 auto", minHeight: 0, overflowY: "auto", padding: 20 }}>
          <label style={labelStyle}>{t("accountSetup.provider", "Provider")} <select name="account-provider" aria-label={t("accountSetup.provider", "Provider")} style={{ ...inputStyle, marginTop: 4 }} value={provider} disabled={loading} onChange={(e) => setProvider(e.target.value)}>
            <option value="gmail">Gmail</option>
            {isFeatureAvailable("imap") && <option value="imap">IMAP / SMTP</option>}
            {isFeatureAvailable("pop3") && <option value="pop3">POP3 / SMTP</option>}
            {isFeatureAvailable("outlook") && <option value="outlook">Microsoft</option>}
          </select></label>
          {provider === "outlook" ? <MicrosoftAccountForm onBusy={(value) => { busy.current = value; setLoading(value); }} onSaved={async () => { await queryClient.invalidateQueries({ queryKey: accountsQueryKey }); if (alive.current) onClose(); }} /> : provider !== "gmail" ? <ProtocolAccountForm key={provider} provider={provider === "pop3" ? "pop3" : "imap"} onBusy={(value) => { busy.current = value; setLoading(value); }} onSaved={async () => { await queryClient.invalidateQueries({ queryKey: accountsQueryKey }); if (alive.current) onClose(); }} /> : <>
          <label htmlFor="setup-account-label" style={{ ...labelStyle, marginTop: 14 }}>{t("accountSetup.accountLabel", "Account label")}</label>
          <input ref={labelRef} id="setup-account-label" style={inputStyle} maxLength={120} disabled={loading}
            value={accountLabel} onChange={(event) => setAccountLabel(event.target.value)} />
          <small style={{ display: "block", marginTop: 6, color: "var(--color-text-secondary)", lineHeight: 1.5 }}>{t("accountSetup.accountLabelHelp", "Only used to identify this account in GoMail; never sent as your sender name.")}</small>
          <div style={{ marginTop: 16 }}>
            <button
              type="button"
              className="settings-button settings-button--primary"
              disabled={loading || !statusLoaded || !configured}
              onClick={() => void handleOAuth()}
            >
              {loading ? t("accountSetup.adding", "Signing in...") : t("accountSetup.signInGoogle", "Sign in with Google")}
            </button>
          </div>
          {!statusLoaded && !statusError && <p role="status">{t("common.loading", "Loading...")}</p>}
          {statusLoaded && !configured && <p role="status">{t("accountConnection.notConfigured", "Configure Google OAuth before reconnecting.")}</p>}
          {statusError && <p role="alert">{t("accountConnection.loadFailed", "Could not load account connection status.")} <button onClick={() => setRetry((value) => value + 1)}>{t("common.retry", "Retry")}</button></p>}
          <GoogleOAuthConfig
            onBusy={(value) => { busy.current = value; setLoading(value); }}
            onChanged={(next) => {
              if (!alive.current) return;
              setConfigured(next.configured);
              setStatusLoaded(true);
              setStatusError(false);
              setError(null);
            }}
          />
          {error && <p role="alert">{error}</p>}
          </>}
          {!isFeatureAvailable("outlook") && <p>Outlook — {unavailableReason()}</p>}
          {!isFeatureAvailable("proxy") && <p>{t("accountSetup.proxyHost", "SOCKS5 Proxy")} — {unavailableReason()}</p>}
        </div>
      </div>
    </div>
  );
}
