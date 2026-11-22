import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { listen } from "@tauri-apps/api/event";
import { useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Clock, RefreshCw, Trash2 } from "lucide-react";
import type { PendingMailOp } from "@/lib/api";
import { dismissFailedPendingMailOps } from "@/lib/api";
import {
  pendingMailOpsQueryKey,
  pendingMailOpsSummaryQueryKey,
  useAccountsQuery,
  usePendingMailOpsQuery,
  usePendingMailOpsSummary,
} from "@/hooks/queries";
import { useMailStore } from "@/stores/mail.store";

const metricStyle: React.CSSProperties = {
  flex: "1 1 132px",
  minWidth: "132px",
  padding: "14px 16px",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--ui-radius-md)",
  background: "var(--color-list-bg)",
};

const tableHeaderStyle: React.CSSProperties = {
  padding: "9px 12px",
  textAlign: "left",
  fontSize: "11.5px",
  fontWeight: 600,
  color: "var(--color-text-secondary)",
  borderBottom: "1px solid var(--color-border)",
  whiteSpace: "nowrap",
};

const tableCellStyle: React.CSSProperties = {
  padding: "10px 12px",
  fontSize: "12.5px",
  color: "var(--color-text-primary)",
  borderBottom: "1px solid var(--color-border)",
  verticalAlign: "top",
};

function formatTimestamp(timestamp: number) {
  return new Date(timestamp * 1000).toLocaleString();
}

function statusTone(status: PendingMailOp["status"]) {
  if (status === "failed") {
    return {
      color: "var(--color-signal-text)",
      background: "var(--color-signal-bg)",
    };
  }
  if (status === "in_progress") {
    return {
      color: "var(--color-accent)",
      background: "var(--color-accent-soft)",
    };
  }
  return {
    color: "var(--color-text-secondary)",
    background: "var(--color-bg-hover)",
  };
}

export default function PendingOpsTab() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const activeAccountId = useMailStore((s) => s.activeAccountId);
  const [selectedAccountId, setSelectedAccountId] = useState<string | null>(activeAccountId);

  const accountsQuery = useAccountsQuery();
  const summaryQuery = usePendingMailOpsSummary(selectedAccountId);
  const opsQuery = usePendingMailOpsQuery(selectedAccountId);

  const accountsById = useMemo(() => {
    return new Map((accountsQuery.data ?? []).map((account) => [account.id, account]));
  }, [accountsQuery.data]);

  useEffect(() => {
    const unlisten = listen("mail:pending-ops-changed", () => {
      queryClient.invalidateQueries({ queryKey: pendingMailOpsSummaryQueryKey(selectedAccountId) });
      queryClient.invalidateQueries({ queryKey: pendingMailOpsQueryKey(selectedAccountId) });
    });
    return () => {
      unlisten.then((fn) => fn());
    };
  }, [queryClient, selectedAccountId]);

  function statusLabel(status: PendingMailOp["status"]) {
    if (status === "failed") return t("pendingOps.statusValue.failed", "Failed");
    if (status === "in_progress") return t("pendingOps.statusValue.inProgress", "Retrying");
    return t("pendingOps.statusValue.pending", "Pending");
  }

  function operationLabel(opType: string) {
    const key = `pendingOps.ops.${opType}`;
    const fallbacks: Record<string, string> = {
      archive: "Archive",
      unarchive: "Unarchive",
      restore: "Restore",
      delete: "Delete",
      delete_permanent: "Delete permanently",
      update_flags: "Update flags",
      move_to_folder: "Move to folder",
      batch_archive: "Batch archive",
      batch_delete: "Batch delete",
      mark_read: "Mark as read",
      send: "Send",
    };
    return t(key, fallbacks[opType] ?? opType);
  }

  function accountLabel(accountId: string) {
    const account = accountsById.get(accountId);
    return account?.email ?? t("pendingOps.unknownAccount", "Unknown account");
  }

  async function refresh() {
    await Promise.all([summaryQuery.refetch(), opsQuery.refetch()]);
  }

  const [dismissError, setDismissError] = useState<string | null>(null);

  async function dismissFailed() {
    setDismissError(null);
    try {
      await dismissFailedPendingMailOps(selectedAccountId);
      await refresh();
    } catch {
      setDismissError(t("pendingOps.dismissFailedError", "Could not dismiss failed writes. Try again."));
    }
  }

  const summary = summaryQuery.data ?? {
    pending_count: 0,
    in_progress_count: 0,
    failed_count: 0,
    total_active_count: 0,
    last_error: null,
    updated_at: null,
  };
  const ops = opsQuery.data ?? [];
  const loading = summaryQuery.isLoading || opsQuery.isLoading;

  return (
    <div>
      <div
        style={{
          display: "flex",
          alignItems: "flex-start",
          justifyContent: "space-between",
          gap: "16px",
          marginBottom: "18px",
        }}
      >
        <div>
          <h2
            style={{
              fontSize: "15px",
              fontWeight: 650,
              color: "var(--color-text-primary)",
              marginTop: 0,
              marginBottom: "6px",
            }}
          >
            {t("pendingOps.queueHeading", "Background write queue")}
          </h2>
          <p
            style={{
              margin: 0,
              fontSize: "12.5px",
              lineHeight: 1.55,
              color: "var(--color-text-secondary)",
              maxWidth: "640px",
            }}
          >
            {t(
              "pendingOps.description",
              "Queued mail changes are retried in the background until the provider accepts them.",
            )}
          </p>
        </div>

        <div style={{ display: "flex", gap: "8px", flexShrink: 0 }}>
          {summary.failed_count > 0 && (
            <button
              type="button"
              onClick={dismissFailed}
              title={t("pendingOps.dismissFailed", "Dismiss failed")}
              aria-label={t("pendingOps.dismissFailed", "Dismiss failed")}
              className="settings-button"
              style={{
                height: "34px",
                padding: "0 12px",
                borderColor: "var(--color-signal-border)",
                background: "var(--color-signal-bg)",
                color: "var(--color-signal-text)",
                fontSize: "12.5px",
              }}
            >
              <Trash2 size={14} />
              {t("pendingOps.dismissFailed", "Dismiss failed")}
            </button>
          )}
          <button
            type="button"
            onClick={refresh}
            title={t("pendingOps.refresh", "Refresh")}
            aria-label={t("pendingOps.refresh", "Refresh")}
            disabled={summaryQuery.isFetching || opsQuery.isFetching}
            className="settings-button"
            style={{
              width: "34px",
              height: "34px",
              padding: 0,
              flexShrink: 0,
            }}
          >
            <RefreshCw size={15} />
          </button>
        </div>
      </div>

      <label
        htmlFor="pending-ops-account"
        style={{
          display: "block",
          fontSize: "12px",
          fontWeight: 600,
          color: "var(--color-text-secondary)",
          marginBottom: "6px",
        }}
      >
        {t("pendingOps.accountFilter", "Account")}
      </label>
      <select
        id="pending-ops-account"
        value={selectedAccountId ?? "all"}
        onChange={(event) => {
          const value = event.target.value;
          setSelectedAccountId(value === "all" ? null : value);
        }}
        style={{
          width: "min(360px, 100%)",
          padding: "8px 12px",
          borderRadius: "var(--ui-radius-md)",
          border: "1px solid var(--color-border)",
          background: "var(--color-bg)",
          color: "var(--color-text-primary)",
          fontSize: "13px",
          marginBottom: "18px",
        }}
      >
        <option value="all">{t("pendingOps.allAccounts", "All accounts")}</option>
        {(accountsQuery.data ?? []).map((account) => (
          <option key={account.id} value={account.id}>
            {account.email}
          </option>
        ))}
      </select>

      <div style={{ display: "flex", gap: "10px", flexWrap: "wrap", marginBottom: "20px" }}>
        <Metric label={t("pendingOps.pending", "Pending")} value={summary.pending_count} />
        <Metric label={t("pendingOps.retrying", "Retrying")} value={summary.in_progress_count} />
        <Metric label={t("pendingOps.failed", "Failed")} value={summary.failed_count} />
        <Metric label={t("pendingOps.active", "Active")} value={summary.total_active_count} />
      </div>

      {dismissError && (
        <div role="alert" style={{ marginBottom: "14px", fontSize: "12.5px", color: "var(--color-signal-text)" }}>
          {dismissError}
        </div>
      )}

      {summary.last_error && (
        <div
          role="alert"
          style={{
            display: "flex",
            gap: "8px",
            alignItems: "flex-start",
            padding: "10px 14px",
            marginBottom: "18px",
            borderRadius: "var(--ui-radius-md)",
            border: "1px solid var(--color-signal-border)",
            background: "var(--color-signal-bg)",
            color: "var(--color-signal-text)",
            fontSize: "12.5px",
            lineHeight: 1.5,
          }}
        >
          <AlertCircle size={15} style={{ marginTop: "2px", flexShrink: 0 }} />
          <span>
            <strong>{t("pendingOps.latestError", "Latest error")}: </strong>
            {summary.last_error}
          </span>
        </div>
      )}

      <h3
        style={{
          fontSize: "13.5px",
          fontWeight: 650,
          marginTop: 0,
          marginBottom: "10px",
          color: "var(--color-text-primary)",
        }}
      >
        {t("pendingOps.queue", "Queue")}
      </h3>

      {loading ? (
        <div style={{ fontSize: "13px", color: "var(--color-text-secondary)" }}>
          {t("common.loading", "Loading...")}
        </div>
      ) : ops.length === 0 ? (
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: "8px",
            padding: "14px 0",
            color: "var(--color-text-secondary)",
            fontSize: "13px",
          }}
        >
          <Clock size={15} />
          <span>{t("pendingOps.empty", "No pending remote writes.")}</span>
        </div>
      ) : (
        <div className="scroll-region pending-ops-table-scroll" style={{ overflowX: "auto", border: "1px solid var(--color-border)", borderRadius: "var(--ui-radius-md)", background: "var(--color-bg)" }}>
          <table style={{ width: "100%", minWidth: "840px", borderCollapse: "collapse" }}>
            <thead>
              <tr style={{ background: "var(--color-list-bg)" }}>
                <th style={tableHeaderStyle}>{t("pendingOps.status", "Status")}</th>
                <th style={tableHeaderStyle}>{t("pendingOps.operation", "Operation")}</th>
                <th style={tableHeaderStyle}>{t("pendingOps.attempts", "Attempts")}</th>
                <th style={tableHeaderStyle}>{t("pendingOps.message", "Message")}</th>
                <th style={tableHeaderStyle}>{t("pendingOps.account", "Account")}</th>
                <th style={tableHeaderStyle}>{t("pendingOps.updated", "Updated")}</th>
                <th style={tableHeaderStyle}>{t("pendingOps.nextRetry", "Next retry")}</th>
                <th style={tableHeaderStyle}>{t("pendingOps.lastError", "Last error")}</th>
              </tr>
            </thead>
            <tbody>
              {ops.map((op) => {
                const tone = statusTone(op.status);
                return (
                  <tr key={op.id}>
                    <td style={tableCellStyle}>
                      <span
                        style={{
                          display: "inline-flex",
                          alignItems: "center",
                          borderRadius: "999px",
                          padding: "2px 8px",
                          fontSize: "11px",
                          fontWeight: 600,
                          color: tone.color,
                          background: tone.background,
                          whiteSpace: "nowrap",
                        }}
                      >
                        {statusLabel(op.status)}
                      </span>
                    </td>
                    <td style={tableCellStyle}>{operationLabel(op.op_type)}</td>
                    <td style={{ ...tableCellStyle, fontVariantNumeric: "tabular-nums" }}>{op.attempts}</td>
                    <td style={{ ...tableCellStyle, fontFamily: "var(--font-mono, monospace)" }}>
                      {op.message_id}
                    </td>
                    <td style={tableCellStyle}>{accountLabel(op.account_id)}</td>
                    <td style={{ ...tableCellStyle, fontVariantNumeric: "tabular-nums" }}>{formatTimestamp(op.updated_at)}</td>
                    <td style={{ ...tableCellStyle, fontVariantNumeric: "tabular-nums" }}>{op.next_retry_at ? formatTimestamp(op.next_retry_at) : "-"}</td>
                    <td style={{ ...tableCellStyle, color: op.last_error ? "var(--color-signal-text)" : "var(--color-text-secondary)" }}>
                      {op.last_error ?? "-"}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <div style={metricStyle}>
      <div
        style={{
          fontSize: "11.5px",
          fontWeight: 600,
          color: "var(--color-text-secondary)",
          marginBottom: "6px",
        }}
      >
        {label}
      </div>
      <div
        style={{
          fontSize: "22px",
          fontWeight: 650,
          lineHeight: 1.05,
          fontVariantNumeric: "tabular-nums",
          color: "var(--color-text-primary)",
        }}
      >
        {value}
      </div>
    </div>
  );
}
