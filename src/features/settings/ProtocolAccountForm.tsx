import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { AccountService, FolderService } from "@/mygo";
import type { ProtocolConfig, ProtocolEndpoint, ProtocolTestResult, ProxySetting, SafeEndpoint } from "@/mygo";
import type { Account } from "@/lib/api";
import { extractErrorMessage } from "@/lib/extractErrorMessage";
import { inputStyle } from "@/styles/form";

export const newEndpoint = (port: number): ProtocolEndpoint => ({ host: "", port, security: "tls", username: "", password: "", auth_mode: "password" });
export function newProtocolConfig(): ProtocolConfig {
  return { version: 1, imap: newEndpoint(993), pop3: newEndpoint(995), smtp: { ...newEndpoint(465), auth_mode: "disabled" }, selected_folders: ["INBOX"], proxy: { mode: "inherit", proxy: null } };
}
const editableEndpoint = (endpoint: SafeEndpoint): ProtocolEndpoint => ({ host: endpoint.host, port: endpoint.port, security: endpoint.security, root_ca_pem: endpoint.root_ca_pem, username: endpoint.username, auth_mode: endpoint.auth_mode, password: "" });

export function ProxyFields({ value, onChange, disabled = false, global = false }: { value: ProxySetting; onChange: (value: ProxySetting) => void; disabled?: boolean; global?: boolean }) {
  const { t } = useTranslation();
  return <fieldset disabled={disabled} style={{ display: "grid", gap: 8 }}><legend>{t("connectionRoute.title", "Connection route")}</legend>
    <label>{t("connectionRoute.mode", "Proxy mode")} <select name="proxy-mode" aria-label={t("connectionRoute.mode", "Proxy mode")} value={value.mode} onChange={(e) => onChange({ mode: e.target.value, proxy: e.target.value === "custom" ? { scheme: "socks5", host: "", port: 1080 } : null })}>
      {!global && <option value="inherit">{t("connectionRoute.inherit", "Inherit global proxy")}</option>}<option value="disabled">{t("connectionRoute.direct", "Direct connection")}</option><option value="custom">{t("connectionRoute.custom", "Custom proxy")}</option>
    </select></label>
    {value.mode === "custom" && value.proxy && <>
      <label>{t("connectionRoute.protocol", "Proxy protocol")} <select aria-label={t("connectionRoute.protocol", "Proxy protocol")} value={value.proxy.scheme} onChange={(e) => onChange({ ...value, proxy: { ...value.proxy!, scheme: e.target.value } })}><option value="socks5">SOCKS5</option><option value="http">HTTP CONNECT</option></select></label>
      <label>{t("connectionRoute.host", "Proxy host")} <input style={inputStyle} value={value.proxy.host} onChange={(e) => onChange({ ...value, proxy: { ...value.proxy!, host: e.target.value } })} /></label>
      <label>{t("connectionRoute.port", "Proxy port")} <input style={inputStyle} type="number" min={1} max={65535} value={value.proxy.port} onChange={(e) => onChange({ ...value, proxy: { ...value.proxy!, port: Number(e.target.value) } })} /></label>
    </>}
    <small>{t("connectionRoute.browserNotice", "The app route applies to protocol and API connections. The system browser follows its own network settings.")}</small>
  </fieldset>;
}

function EndpointFields({ name, value, onChange, microsoft = false }: { name: string; value: ProtocolEndpoint; onChange: (value: ProtocolEndpoint) => void; microsoft?: boolean }) {
  const { t } = useTranslation();
  const set = (patch: Partial<ProtocolEndpoint>) => onChange({ ...value, ...patch });
  const [trust, setTrust] = useState(!!value.root_ca_pem);
  return <fieldset style={{ display: "grid", gap: 8 }}><legend>{name}</legend>
    <label>{t("protocolAccount.authentication", `${name} authentication`, { protocol: name })} <select name={`${name}-authentication`} aria-label={t("protocolAccount.authentication", `${name} authentication`, { protocol: name })} value={value.auth_mode} onChange={(e) => set({ auth_mode: e.target.value })}>
      {(name === "SMTP" || microsoft) && <option value="disabled">{t("common.disabled", "Disabled")}</option>}{!microsoft && <option value="password">{t("protocolAccount.passwordAuth", "Password / app password")}</option>}{microsoft && <option value="xoauth2">{t("protocolAccount.microsoftAuth", "Microsoft OAuth (authorize separately)")}</option>}
    </select></label>
    {value.auth_mode !== "disabled" && <>
      <label>{t("protocolAccount.host", `${name} host`, { protocol: name })} <input name={`${name}-host`} style={inputStyle} value={value.host} onChange={(e) => set({ host: e.target.value })} required /></label>
      <label>{t("protocolAccount.port", `${name} port`, { protocol: name })} <input name={`${name}-port`} style={inputStyle} type="number" min={1} max={65535} value={value.port} onChange={(e) => set({ port: Number(e.target.value) })} required /></label>
      <label>{t("protocolAccount.security", `${name} security`, { protocol: name })} <select aria-label={t("protocolAccount.security", `${name} security`, { protocol: name })} value={value.security} onChange={(e) => set({ security: e.target.value })}><option value="tls">TLS</option><option value="starttls">{t("protocolAccount.starttlsRequired", `${name === "POP3" ? "STLS" : "STARTTLS"} (required)`, { protocol: name === "POP3" ? "STLS" : "STARTTLS" })}</option></select></label>
      <label>{t("protocolAccount.username", `${name} username`, { protocol: name })} <input name={`${name}-username`} style={inputStyle} value={value.username} onChange={(e) => set({ username: e.target.value })} required autoComplete="username" /></label>
      {value.auth_mode === "password" && <label>{t("protocolAccount.password", `${name} password`, { protocol: name })} <input name={`${name}-password`} style={inputStyle} type="password" autoComplete="new-password" value={value.password ?? ""} onChange={(e) => set({ password: e.target.value })} placeholder={t("protocolAccount.keepPassword", "Leave blank when editing to keep the stored password")} /></label>}
      <label><input type="checkbox" checked={trust} onChange={(e) => { setTrust(e.target.checked); if (!e.target.checked) set({ root_ca_pem: "" }); }} />{t("protocolAccount.trustCA", "Trust an additional CA for this endpoint")}</label>
      {trust && <label>{t("protocolAccount.trustedCA", `${name} trusted CA (PEM)`, { protocol: name })}<textarea name={`${name}-ca`} style={inputStyle} value={value.root_ca_pem ?? ""} onChange={(e) => set({ root_ca_pem: e.target.value })} required rows={4} /><small>{t("protocolAccount.trustNotice", "Only add a CA you have independently verified. Hostname verification remains required.")}</small></label>}
    </>}
  </fieldset>;
}

/** Secrets live only in the active form; safe settings never hydrate passwords. */
export default function ProtocolAccountForm({ account, provider: initialProvider = "imap", onSaved, onBusy }: { account?: Account; provider?: "imap" | "pop3"; onSaved: (account: Account) => void; onBusy?: (busy: boolean) => void }) {
  const { t } = useTranslation();
  const [provider, setProvider] = useState(initialProvider);
  const [email, setEmail] = useState(account?.email ?? "");
  const [displayName, setDisplayName] = useState(account?.display_name ?? "");
  const [config, setConfig] = useState(newProtocolConfig);
  const [revision, setRevision] = useState("");
  const [microsoft, setMicrosoft] = useState(false);
  const [ready, setReady] = useState(!account);
  const [busy, setBusy] = useState(false);
  const lock = useRef(false);
  const alive = useRef(true);
  const [error, setError] = useState("");
  const [loadFailed, setLoadFailed] = useState(false);
  const [report, setReport] = useState<ProtocolTestResult | null>(null);
  const [folders, setFolders] = useState<{ remote_id: string; name: string }[]>([]);
  useEffect(() => {
    let active = true;
    alive.current = true;
    setReady(!account); setError(""); setLoadFailed(false); setRevision("");
    if (account) void AccountService.protocolSettings({ accountId: account.id }).then((settings) => {
      if (!active) return;
      setConfig({ version: 1, imap: editableEndpoint(settings.imap), smtp: editableEndpoint(settings.smtp), pop3: editableEndpoint(settings.pop3), selected_folders: settings.selected_folders ?? [], proxy: settings.proxy });
      setProvider(account.provider === "pop3" ? "pop3" : "imap");
      setMicrosoft(!!settings.microsoft_client_id);
      setRevision(settings.revision); setReady(true);
    }).catch(() => { if (active) setLoadFailed(true); });
    return () => { active = false; alive.current = false; };
  }, [account]);
  async function act(action: "save" | "test" | "folders") {
    if (lock.current || !ready) return;
    lock.current = true; setBusy(true); onBusy?.(true); setError(""); setReport(null);
    try {
      if (action === "folders" && account) {
        const result = await FolderService.list({ accountId: account.id });
        if (alive.current) setFolders(result);
        return;
      }
      const request = { email, display_name: displayName, provider, imap_host: "", imap_port: 0, smtp_host: "", smtp_port: 0, username: "", password: "", imap_security: "tls", smtp_security: "tls", accept_invalid_certs: false, allow_plaintext: false, config };
      if (action === "test") {
        const result = account ? microsoft ? await AccountService.testStored({ accountId: account.id }) : await AccountService.testProtocolSettings({ accountId: account.id, revision, displayName, config }) : await AccountService.test(request);
        if (alive.current) setReport(result);
      } else {
        const saved = account ? await AccountService.saveProtocol({ accountId: account.id, revision, displayName, config }) : await AccountService.add(request);
        if (alive.current) onSaved(saved as Account);
      }
    } catch (err) { if (alive.current) setError(extractErrorMessage(err)); }
    finally { lock.current = false; if (alive.current) { setBusy(false); onBusy?.(false); } }
  }
  return <form id="protocol-account-settings" aria-label={t("protocolAccount.title", "Protocol account settings")} onSubmit={(e) => { e.preventDefault(); void act("save"); }} style={{ display: "grid", gap: 12 }}>
    <fieldset disabled={busy || !ready} style={{ display: "grid", gap: 12, border: 0, padding: 0 }}>
      {!account && <label>{t("accountSetup.incomingProtocol", "Incoming protocol")} <select value={provider} onChange={(e) => setProvider(e.target.value as "imap" | "pop3")}><option value="imap">IMAP</option><option value="pop3">POP3</option></select></label>}
      <label>{t("protocolAccount.email", "Email")} <input name="email" style={inputStyle} type="email" value={email} disabled={!!account} required onChange={(e) => setEmail(e.target.value)} /></label>
      <label>{t("protocolAccount.displayName", "Sender display name")} <input name="display-name" style={inputStyle} value={displayName} maxLength={256} onChange={(e) => setDisplayName(e.target.value)} /></label>
      <EndpointFields key={`${provider}-${ready}`} microsoft={microsoft} name={provider === "pop3" ? "POP3" : "IMAP"} value={config[provider]} onChange={(endpoint) => setConfig({ ...config, [provider]: endpoint })} />
      <EndpointFields key={`smtp-${ready}`} microsoft={microsoft} name="SMTP" value={config.smtp} onChange={(endpoint) => setConfig({ ...config, smtp: endpoint })} />
      {provider === "imap" && config.imap.auth_mode !== "disabled" && <fieldset><legend>{t("protocolAccount.selectedFolders", "Selected folders")}</legend>
        <label>{t("protocolAccount.folderNames", "Remote folder names (one per line)")}<textarea name="selected-folders" style={inputStyle} rows={3} value={(config.selected_folders ?? []).join("\n")} onChange={(e) => setConfig({ ...config, selected_folders: e.target.value.split("\n").filter(Boolean) })} /></label>
        {account && <button type="button" onClick={() => void act("folders")}>{t("protocolAccount.chooseFolders", "Choose cached folders")}</button>}
        {folders.map((folder) => <label key={folder.remote_id} style={{ display: "block" }}><input type="checkbox" checked={config.selected_folders?.includes(folder.remote_id) ?? false} onChange={(e) => setConfig({ ...config, selected_folders: e.target.checked ? [...(config.selected_folders ?? []), folder.remote_id] : config.selected_folders?.filter((id) => id !== folder.remote_id) })} />{folder.name}</label>)}
        <small>{t("protocolAccount.folderNotice", "Only the selected folders sync. Use exact server names; INBOX is the default.")}</small>
      </fieldset>}
      {provider === "pop3" && <p>{t("protocolAccount.pop3Notice", "POP3 downloads Inbox messages by UIDL. Server folders, flag changes, moves and trash are unavailable. Sending requires a separate SMTP configuration.")}</p>}
      <ProxyFields value={config.proxy} onChange={(proxy) => setConfig({ ...config, proxy })} />
      <small>{t("protocolAccount.securityNotice", "TLS verification is required. Plaintext and blanket certificate bypass are unavailable. Saving does not start an automatic sync.")}</small>
      {microsoft && <small>{t("protocolAccount.savedTestNotice", "The connection test uses saved settings and persists refreshed grants before use. Save edited endpoints or routing before testing.")}</small>}
      <div><button type="button" name="test-connection" onClick={() => void act("test")}>{microsoft ? t("protocolAccount.testSaved", "Test saved connection") : t("protocolAccount.testEntered", "Test entered settings")}</button> <button type="submit">{busy ? t("protocolAccount.working", "Working...") : account ? t("protocolAccount.save", "Save protocol settings") : t("protocolAccount.add", "Add protocol account")}</button></div>
    </fieldset>
    {!ready && !loadFailed && <p role="status">{t("protocolAccount.loading", "Loading saved settings...")}</p>}
    {report && <p role="status">{report.success ? t("protocolAccount.connectionVerified", "Connection verified") : report.message}</p>}
    {loadFailed && <p role="alert">{t("protocolAccount.loadFailed", "Could not load saved settings. Close and reopen before editing.")}</p>}
    {error && <p role="alert">{error}</p>}
  </form>;
}
