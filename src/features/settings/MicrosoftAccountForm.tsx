import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { MicrosoftService } from "@/mygo";
import type { MicrosoftStatus, ProxySetting } from "@/mygo";
import type { Account } from "@/lib/api";
import { ProxyFields } from "./ProtocolAccountForm";
import { extractErrorMessage } from "@/lib/extractErrorMessage";
import { inputStyle } from "@/styles/form";

/** Microsoft is a public client: no client secret or pasted token is collected. */
export default function MicrosoftAccountForm({ account, onSaved, onBusy }: { account?: Account; onSaved: (account: Account) => void; onBusy?: (busy: boolean) => void }) {
  const { t } = useTranslation();
  const [clientId, setClientId] = useState("");
  const [tenant, setTenant] = useState("common");
  const [audience, setAudience] = useState("all");
  const [graph, setGraph] = useState(false);
  const [imap, setImap] = useState(true);
  const [smtp, setSmtp] = useState(false);
  const [displayName, setDisplayName] = useState(account?.display_name ?? "");
  const [proxy, setProxy] = useState<ProxySetting>({ mode: "inherit", proxy: null });
  const [status, setStatus] = useState<MicrosoftStatus | null>(null);
  const [ready, setReady] = useState(!account);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loadFailed, setLoadFailed] = useState(false);
  const [cancelFailed, setCancelFailed] = useState(false);
  const lock = useRef(false);
  const alive = useRef(true);
  useEffect(() => {
    let active = true; alive.current = true;
    if (account) void MicrosoftService.status({ accountId: account.id }).then((value) => {
      if (!active) return;
      setStatus(value); setClientId(value.clientId); setTenant(value.tenant); setAudience(value.audience);
      setGraph(value.graph); setImap(value.imap); setSmtp(value.smtp); setReady(true);
    }).catch(() => { if (active) setLoadFailed(true); });
    return () => { active = false; alive.current = false; };
  }, [account]);
  async function authorize() {
    if (!ready || lock.current) return;
    lock.current = true; setBusy(true); onBusy?.(true); setError(""); setCancelFailed(false);
    try {
      const saved = await MicrosoftService.authorize({ accountId: account?.id, displayName, clientId: clientId.trim(), tenant: tenant.trim(), audience, resource: imap || smtp ? "exchange" : "graph", graph, imap, smtp, proxy });
      if (alive.current) onSaved(saved as Account);
    } catch (err) { if (alive.current) setError(extractErrorMessage(err)); }
    finally { lock.current = false; if (alive.current) { setBusy(false); onBusy?.(false); } }
  }
  function accessStateLabel(state: string) {
    switch (state) {
      case "reauth_required": return t("microsoftAccount.reauthRequired", "Sign in again");
      case "permission_required": return t("microsoftAccount.permissionRequired", "Permission required");
      case "configuration_required": return t("microsoftAccount.configurationRequired", "Configuration requires attention");
      case "credentials_unavailable": return t("microsoftAccount.credentialsUnavailable", "Saved credentials unavailable");
      case "temporary_error": return t("microsoftAccount.temporaryError", "Temporary connection problem");
      case "reconnecting": return t("microsoftAccount.reconnecting", "Sign-in in progress");
      default: return t("microsoftAccount.accessUnavailable", "Authorization requires attention");
    }
  }
  return <form aria-label={t("microsoftAccount.title", "Microsoft authorization")} style={{ display: "grid", gap: 12 }} onSubmit={(e) => { e.preventDefault(); void authorize(); }}>
    <p>{t("microsoftAccount.setupNotice", "Register a native public client with a loopback redirect in Microsoft Entra. The system browser requests consent; credentials stay with Microsoft. IMAP and SMTP require their own Exchange scopes and tenant policy.")}</p>
    <fieldset disabled={!ready || busy} style={{ display: "grid", gap: 10 }}><legend>{t("microsoftAccount.policy", "Application and resource policy")}</legend>
      <label>{t("microsoftAccount.clientId", "Public client ID")} <input style={inputStyle} required value={clientId} disabled={!!account} onChange={(e) => setClientId(e.target.value)} /></label>
      <label>{t("microsoftAccount.audience", "Account audience")} <select value={audience} disabled={!!account} onChange={(e) => { const value = e.target.value; setAudience(value); setTenant(value === "all" ? "common" : value === "personal" ? "consumers" : value === "organizations" ? "organizations" : ""); }}><option value="all">{t("microsoftAccount.audienceAll", "Personal and organizational accounts")}</option><option value="organizations">{t("microsoftAccount.audienceOrganizations", "Organizations")}</option><option value="personal">{t("microsoftAccount.audiencePersonal", "Personal accounts")}</option><option value="single_tenant">{t("microsoftAccount.audienceSingleTenant", "Single tenant")}</option></select></label>
      <label>{t("microsoftAccount.tenant", "Tenant")} <input style={inputStyle} required value={tenant} disabled={!!account} onChange={(e) => setTenant(e.target.value)} /></label>
      <label>{t("protocolAccount.displayName", "Sender display name")} <input style={inputStyle} value={displayName} onChange={(e) => setDisplayName(e.target.value)} /></label>
      <label><input type="checkbox" checked={graph} onChange={(e) => setGraph(e.target.checked)} />{t("microsoftAccount.graph", "Experimental Graph mail (read/write/send)")}</label>
      <label><input type="checkbox" checked={imap} onChange={(e) => setImap(e.target.checked)} />IMAP XOAUTH2</label>
      <label><input type="checkbox" checked={smtp} onChange={(e) => setSmtp(e.target.checked)} />SMTP XOAUTH2</label>
      <small>{t("microsoftAccount.resourceNotice", "Enabling SMTP is separate from IMAP. Tenant SMTP AUTH restrictions can still reject sending. Graph 202 and SMTP acceptance are not proof of recipient delivery.")}</small>
      {!account && <ProxyFields value={proxy} onChange={setProxy} />}
      <button type="submit" disabled={!clientId.trim() || (!graph && !imap && !smtp)}>{t("microsoftAccount.authorize", "Authorize selected resources")}</button>
    </fieldset>
    {busy && <><p role="status">{t("microsoftAccount.waiting", "Waiting for Microsoft consent...")}</p><button type="button" onClick={() => void MicrosoftService.cancel({ accountId: account?.id ?? "" }).catch(() => { if (alive.current) setCancelFailed(true); })}>{t("microsoftAccount.cancel", "Cancel authorization")}</button></>}
    {status && <div role="status">
      {(["Graph", "IMAP", "SMTP"] as const).map((resource) => {
        const authorized = resource === "Graph" ? status.graphAuthorized : resource === "IMAP" ? status.imapAuthorized : status.smtpAuthorized;
        const label = authorized ? t("microsoftAccount.authorized", "authorized") : t("microsoftAccount.notAuthorized", "not authorized");
        return <p key={resource}>{t("microsoftAccount.resourceStatus", `${resource}: ${label}`, { resource, status: label })}</p>;
      })}
      <p>{t("microsoftAccount.protocolNotice", "Authorization does not verify protocol login.")}</p>
      {status.sendState && status.sendState !== "available" && <p>{t("microsoftAccount.sendState", `Outgoing authorization: ${accessStateLabel(status.sendState)}.`, { state: accessStateLabel(status.sendState) })} {t("microsoftAccount.sendRepair", "Reauthorize selected resources to repair sending; receive access remains independent. Unknown sends are never retried automatically.")}</p>}
      {status.writeState && status.writeState !== "available" && <p>{t("microsoftAccount.writeState", `Mail change authorization: ${accessStateLabel(status.writeState)}.`, { state: accessStateLabel(status.writeState) })} {t("microsoftAccount.writeRepair", "Reauthorize selected resources to repair queued flag and folder changes; receive and send access remain independent.")}</p>}
    </div>}
    {!ready && !loadFailed && <p role="status">{t("microsoftAccount.loading", "Loading authorization status...")}</p>}
    {loadFailed && <p role="alert">{t("microsoftAccount.loadFailed", "Could not load Microsoft authorization status. Reopen before changing authorization.")}</p>}
    {busy && cancelFailed && <p role="alert">{t("microsoftAccount.cancelFailed", "Could not cancel authorization. Close the browser flow to finish.")}</p>}
    {error && <p role="alert">{error}</p>}
  </form>;
}
