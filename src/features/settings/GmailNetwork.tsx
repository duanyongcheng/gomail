import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { AccountService } from "@/mygo";
import type { ProxySetting } from "@/mygo";
import type { Account } from "@/lib/api";
import { ProxyFields } from "./ProtocolAccountForm";
import { extractErrorMessage } from "@/lib/extractErrorMessage";

function GmailNetworkAccount({ account }: { account: Account }) {
  const { t } = useTranslation();
  const [setting, setSetting] = useState<ProxySetting>({ mode: "inherit", proxy: null });
  const [ready, setReady] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loadFailed, setLoadFailed] = useState(false);
  const [saved, setSaved] = useState(false);
  const alive = useRef(true);
  const lock = useRef(false);
  useEffect(() => {
    let active = true; alive.current = true;
    void AccountService.accountNetwork({ accountId: account.id }).then((value) => { if (active) { setSetting(value); setReady(true); } }).catch(() => { if (active) setLoadFailed(true); });
    return () => { active = false; alive.current = false; };
  }, [account.id]);
  async function save() {
    if (!ready || lock.current) return;
    lock.current = true; setBusy(true); setError(""); setSaved(false);
    try {
      await AccountService.setAccountNetwork({ accountId: account.id, setting });
      if (alive.current) setSaved(true);
    } catch (err) { if (alive.current) setError(extractErrorMessage(err)); }
    finally { lock.current = false; if (alive.current) setBusy(false); }
  }
  return <section style={{ display: "grid", gap: 8, marginTop: 16 }} aria-label={t("connectionRoute.gmailAccountTitle", `${account.email} — Gmail connection route`, { email: account.email })}>
    <h3>{t("connectionRoute.gmailAccountTitle", `${account.email} — Gmail connection route`, { email: account.email })}</h3>
    <ProxyFields value={setting} disabled={!ready || busy} onChange={(value) => { setSetting(value); setSaved(false); }} />
    <button disabled={!ready || busy} onClick={() => void save()}>{busy ? t("common.saving", "Saving...") : t("connectionRoute.saveGmail", "Save Gmail account route")}</button>
    {saved && <p role="status">{t("connectionRoute.accountSaved", "Account route saved. Sync scheduling follows your existing preference.")}</p>}
    {loadFailed && <p role="alert">{t("connectionRoute.accountLoadFailed", "Could not load this account's connection route. Reopen settings before saving.")}</p>}
    {error && <p role="alert">{error}</p>}
  </section>;
}

export default function GmailNetwork({ accounts }: { accounts: Account[] }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return <div style={{ marginTop: 16 }}><button aria-expanded={open} onClick={() => setOpen(!open)}>{t("connectionRoute.gmailAccounts", "Gmail account connection routes")}</button>{open && accounts.map((account) => <GmailNetworkAccount key={account.id} account={account} />)}</div>;
}
