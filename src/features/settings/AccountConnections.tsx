import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { Account } from "@/lib/api";
import { AccountService, type AccountConnection, type ConnectionState } from "@/mygo";
import { accountLabel } from "@/lib/accountIdentity";

const buttonStyle = { padding: "6px 10px", border: "1px solid var(--color-border)", borderRadius: 6, background: "var(--color-bg)", color: "var(--color-text-primary)", fontSize: 12 };

export default function AccountConnections({ accounts }: { accounts: Account[] }) {
  const { t } = useTranslation();
  const query = useQuery({
    queryKey: ["account-connections", accounts.map((account) => account.id)],
    queryFn: () => AccountService.connections({}),
    networkMode: "always", // Connection state is read from local account metadata.
    refetchInterval: 3000,
  });
  return <section aria-labelledby="account-connections-title" style={{ marginTop: 24, fontSize: 12 }}>
    <h3 id="account-connections-title" style={{ fontSize: 14, fontWeight: 600 }}>{t("accountConnection.title", "Gmail connection")}</h3>
    <p style={{ color: "var(--color-text-secondary)" }}>{t("accountConnection.notice", "Status is read locally. Reconnect opens Google sign-in for the same mailbox and keeps your mail and drafts. Unknown sends are never resent automatically.")}</p>
    {query.isPending && <p role="status">{t("common.loading", "Loading...")}</p>}
    {query.isError && <p role="alert">{t("accountConnection.loadFailed", "Could not read account connection states.")} <button onClick={() => void query.refetch()}>{t("common.retry", "Retry")}</button></p>}
    {query.data && accounts.map((account) => <ConnectionRow key={account.id} account={account} connection={query.data.find((item) => item.accountId === account.id)} />)}
  </section>;
}

function ConnectionRow({ account, connection }: { account: Account; connection?: AccountConnection }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const acting = useRef(false);
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const labels: Record<ConnectionState, string> = {
    available: t("accountConnection.available", "Local credentials available"),
    temporary_error: t("accountConnection.temporaryError", "Temporary connection problem; retries continue"),
    reauth_required: t("accountConnection.reauthRequired", "Authorization expired or revoked; reconnect this account"),
    permission_required: t("accountConnection.permissionRequired", "Mail access permission is missing; reconnect and grant access"),
    credentials_unavailable: t("accountConnection.credentialsUnavailable", "Saved credentials unavailable; unlock the profile keyring and reconnect"),
    configuration_required: t("accountConnection.configurationRequired", "Google sign-in configuration requires attention"),
    reconnecting: t("accountConnection.reconnecting", "Waiting for Google sign-in; this account's queued work is paused"),
  };
  const pending = busy || connection?.state === "reconnecting";
  async function reconnect() {
    if (acting.current) return;
    acting.current = true; setBusy(true); setError(""); setDone(false);
    try {
      await AccountService.reconnect({ accountId: account.id });
      if (alive.current) setDone(true);
    } catch (err) {
      if (alive.current) setError(err instanceof Error ? err.message : t("accountConnection.failed", "Could not reconnect. Existing mail and credentials are kept."));
    } finally {
      await Promise.all(["account-connections", "accounts", "outbox", "pendingMailOps", "pendingMailOpsList"].map((key) => client.invalidateQueries({ queryKey: [key] })));
      acting.current = false;
      if (alive.current) { setBusy(false); setCancelling(false); }
    }
  }
  async function cancel() {
    if (cancelling) return;
    setCancelling(true); setError("");
    try { await AccountService.cancelReconnect({ accountId: account.id }); }
    catch { if (alive.current) { setError(t("accountConnection.cancelFailed", "Could not cancel sign-in. Try again.")); setCancelling(false); } }
  }
  return <div data-connection-account={account.id} style={{ borderBottom: "1px solid var(--color-border)", padding: "12px 0", overflowWrap: "anywhere" }}>
    <strong>{accountLabel(account)}</strong>
    <p role="status">{pending ? labels.reconnecting : connection ? labels[connection.state] : t("accountConnection.unavailable", "Connection status unavailable")}</p>
    <button type="button" style={buttonStyle} disabled={pending || !connection?.canReconnect} aria-label={`${t("accountConnection.reconnect", "Reconnect")}: ${account.email}`} onClick={() => void reconnect()}>{t("accountConnection.reconnect", "Reconnect")}</button>
    {pending && <button type="button" style={{ ...buttonStyle, marginLeft: 8 }} disabled={cancelling} aria-label={`${t("accountConnection.cancel", "Cancel sign-in")}: ${account.email}`} onClick={() => void cancel()}>{t("accountConnection.cancel", "Cancel sign-in")}</button>}
    {connection && !connection.canReconnect && <p>{t("accountConnection.notConfigured", "Configure Google OAuth before reconnecting.")}</p>}
    {done && <p role="status">{t("accountConnection.done", "Account reconnected. Your existing mail and drafts are kept.")}</p>}
    {error && <p role="alert">{error}</p>}
  </div>;
}
