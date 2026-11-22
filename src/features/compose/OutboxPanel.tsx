import { useEffect, useRef, useState } from "react";
import { listen } from "@tauri-apps/api/event";
import { useAccountsQuery } from "@/hooks/queries";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { OutboxService, type OutboxItem } from "@/mygo";
import { useConfirmStore } from "@/stores/confirm.store";
import { extractErrorMessage } from "@/lib/extractErrorMessage";

const legacyCancelUnavailable = "cannot cancel this older linked send: its local draft is unavailable; queued content was preserved";

export default function OutboxPanel({ accountId = "" }: { accountId?: string }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const { data: accounts = [] } = useAccountsQuery();
  const [filter, setFilter] = useState("");
  const selectedAccount = accountId || filter;
  const query = useQuery({
    queryKey: ["outbox", selectedAccount],
    queryFn: () => OutboxService.list({ accountId: selectedAccount }),
    networkMode: "always", // Local encrypted summaries; Check Sent stays explicit.
    refetchInterval: 3000,
  });
  useEffect(() => {
    const unlisten = listen("mail:outbox-changed", () => { void client.invalidateQueries({ queryKey: ["outbox"] }); });
    return () => { void unlisten.then((off) => off()); };
  }, [client]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const acting = useRef(false);
  const statusLabel = (status: string) => t(`outbox.status.${status}`, status);
  async function action(item: OutboxItem, action: string) {
    if (acting.current) return;
    if (action === "dismiss" && item.status === "outcome_unknown" && item.cloudDraft) return;
    acting.current = true; setBusy(true); setError("");
    try {
      if (action === "cancel" || action === "dismiss") {
        if (!await useConfirmStore.getState().confirm({
          title: t(`outbox.${action}`, action),
          message: item.status === "outcome_unknown"
            ? t("outbox.unknownWarning", "This may already have been sent. Dismissing does not undo delivery. Check Sent before composing another copy.")
            : action === "cancel" && item.cloudDraft
              ? t("outbox.cloudCancelWarning", "Cancel this queued send? Your current local draft and attachments will be kept for editing. This does not change the draft in Gmail.")
              : t("outbox.removeWarning", "Remove this local outbox item?"),
          confirmLabel: t(`outbox.${action}`, action),
        })) return;
      }
      await OutboxService.action({ accountId: item.accountId, id: item.id, action });
      await client.invalidateQueries({ queryKey: ["outbox"] });
    } catch (err) {
      // Match only this sanitized backend contract; never display raw errors.
      setError(action === "cancel" && extractErrorMessage(err) === legacyCancelUnavailable
        ? t("outbox.legacyCancelUnavailable", "This older send could not be cancelled because its local draft is unavailable. The queued content has been kept in Outbox. Gmail was not changed.")
        : action === "check" ? t("outbox.checkInconclusive", "Sent check is inconclusive. No resend was attempted.") : t("outbox.actionFailed", "Could not change this outbox item. Refresh and try again."));
    } finally { acting.current = false; setBusy(false); }
  }
  return <details className="outbox-drawer">
    <summary className="outbox-drawer-summary">
      {t("outbox.title", "Outbox")} ({query.data?.length ?? 0})
    </summary>
    <div className="outbox-drawer-body">
      <div className="outbox-drawer-controls">
        {!accountId && <label style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12.5 }}>{t("pendingOps.accountFilter", "Account")} <select value={filter} onChange={(e) => setFilter(e.target.value)} style={{ padding: "5px 8px", borderRadius: "var(--ui-radius-sm)", border: "1px solid var(--color-border)", background: "var(--color-bg)", color: "var(--color-text-primary)" }}>
          <option value="">{t("outbox.allAccounts", "All accounts")}</option>
          {accounts.map((a) => <option key={a.id} value={a.id}>{a.email}</option>)}
        </select></label>}
        <button type="button" className="settings-button" disabled={busy || query.isFetching} onClick={() => void query.refetch()}>{t("pendingOps.refresh", "Refresh")}</button>
      </div>
      <p className="outbox-drawer-note">{t("outbox.notice", "Queued is not sent. Unknown outcomes are never resent automatically.")}</p>
      <p className="outbox-drawer-note">{t("outbox.listLimit", "Showing up to 50 items, active first. Dismiss finished items to see older ones.")}</p>
      {error && <p role="alert" style={{ margin: 0, color: "var(--color-error)", fontSize: 12.5 }}>{error}</p>}
      {query.isLoading ? <p role="status" className="outbox-drawer-note">{t("common.loading", "Loading...")}</p> : query.isError ? <p role="alert" style={{ margin: 0, color: "var(--color-error)", fontSize: 12.5 }}>{t("outbox.readFailed", "Could not read outbox.")}</p> : !query.data?.length ? <p className="outbox-drawer-note">{t("outbox.empty", "No outbox items")}</p> : <ul className="outbox-list">
        {query.data.map((item) => {
          const isSignal = item.status === "outcome_unknown" || item.status === "failed";
          return <li key={item.id} className={`outbox-item${isSignal ? " outbox-item--signal" : ""}`}>
            <div className="outbox-item-head">
              <strong>{item.subject || t("compose.untitledDraft", "Untitled draft")}</strong>
              {!accountId && <span style={{ color: "var(--color-text-secondary)", fontSize: 12 }}>· {accounts.find((a) => a.id === item.accountId)?.email}</span>}
              <span className={`outbox-status-pill${isSignal ? " outbox-status-pill--signal" : ""}`}>{statusLabel(item.status)}</span>
              <span style={{ color: "var(--color-text-secondary)", fontSize: 12, fontVariantNumeric: "tabular-nums" }}>{t("outbox.attempts", "Attempts")}: {item.attempts}</span>
              {item.nextRetryAt && <span style={{ color: "var(--color-text-tertiary)", fontSize: 11.5, fontVariantNumeric: "tabular-nums" }}>· {new Date(item.nextRetryAt * 1000).toLocaleString()}</span>}
            </div>
            {item.lastError && <p className="outbox-drawer-note" style={{ color: "var(--color-signal-text)" }}>{item.lastError}</p>}
            {item.status === "outcome_unknown" && item.cloudDraft && <p className="outbox-drawer-note" style={{ color: "var(--color-signal-text)" }}>{t("outbox.cloudUnknown", "This send is linked to a Gmail draft. Its reconciliation record is kept until Check Sent confirms the result; do not send or upload another copy.")}</p>}
            <div className="outbox-item-actions">
              {item.status === "failed" && <button type="button" className="settings-button settings-button--primary" disabled={busy} onClick={() => void action(item, "retry")}>{t("outbox.retry", "Retry")}</button>}
              {["queued", "failed"].includes(item.status) && <button type="button" className="settings-button" disabled={busy} onClick={() => void action(item, "cancel")}>{t("outbox.cancel", "Cancel queued send")}</button>}
              {item.status === "outcome_unknown" && <button type="button" className="settings-button settings-button--primary" disabled={busy} onClick={() => void action(item, "check")}>{t("outbox.check", "Check Sent")}</button>}
              {(item.status === "sent" || (item.status === "outcome_unknown" && !item.cloudDraft)) && <button type="button" className="settings-button" disabled={busy} onClick={() => void action(item, "dismiss")}>{t("outbox.dismiss", "Dismiss")}</button>}
            </div>
          </li>;
        })}
      </ul>}
    </div>
  </details>;
}
