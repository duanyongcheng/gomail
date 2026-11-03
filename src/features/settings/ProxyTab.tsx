import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { AccountService } from "@/mygo";
import type { ProxySetting } from "@/mygo";
import { ProxyFields } from "./ProtocolAccountForm";
import { extractErrorMessage } from "@/lib/extractErrorMessage";

export default function ProxyTab() {
  const { t } = useTranslation();
  const [value, setValue] = useState<ProxySetting>({ mode: "disabled", proxy: null });
  const [ready, setReady] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loadFailed, setLoadFailed] = useState(false);
  const [saved, setSaved] = useState(false);
  const lock = useRef(false);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    void AccountService.network({}).then((settings) => {
      if (alive.current) { setValue({ mode: settings.proxy ? "custom" : "disabled", proxy: settings.proxy }); setReady(true); }
    }).catch(() => { if (alive.current) setLoadFailed(true); });
    return () => { alive.current = false; };
  }, []);
  async function save() {
    if (!ready || lock.current) return;
    lock.current = true; setBusy(true); setSaved(false); setError("");
    try {
      await AccountService.setNetwork({ proxy: value.mode === "custom" ? value.proxy : null });
      if (alive.current) setSaved(true);
    } catch (err) { if (alive.current) setError(extractErrorMessage(err)); }
    finally { lock.current = false; if (alive.current) setBusy(false); }
  }
  return <div style={{ display: "grid", gap: 12 }}>
    <h2>{t("connectionRoute.globalTitle", "Global proxy")}</h2>
    <p>{t("connectionRoute.globalNotice", "Choose a SOCKS5 or HTTP CONNECT proxy for accounts that inherit the global route. A failed proxy never falls back to direct. Per-account overrides are in Edit account.")}</p>
    <ProxyFields value={value} onChange={(next) => { setValue(next); setSaved(false); }} disabled={!ready || busy} global />
    <button disabled={!ready || busy} onClick={() => void save()}>{busy ? t("common.saving", "Saving...") : t("connectionRoute.saveGlobal", "Save global proxy")}</button>
    {!ready && !loadFailed && <p role="status">{t("common.loading", "Loading...")}</p>}
    {saved && <p role="status">{t("connectionRoute.globalSaved", "Global proxy saved.")}</p>}
    {loadFailed && <p role="alert">{t("connectionRoute.globalLoadFailed", "Could not load network settings. Reopen this tab before saving.")}</p>}
    {error && <p role="alert">{error}</p>}
  </div>;
}
