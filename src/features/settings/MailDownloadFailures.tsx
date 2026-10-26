import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { listen } from "@tauri-apps/api/event";
import { listDownloadFailures, retryDownloads } from "@/lib/mailDownloads";
import type { Account } from "@/lib/api";
import { inputStyle } from "@/styles/form";

const pageSize = 20;
const queryKey = ["mail-download-failures"];
const buttonStyle = {
  border: "1px solid var(--color-border)", borderRadius: "6px", padding: "6px 10px",
  background: "var(--color-bg)", color: "var(--color-text-primary)", fontSize: "12px",
};

export default function MailDownloadFailures({ accounts }: { accounts: Account[] }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [selectedAccount, setSelectedAccount] = useState("");
  const [offset, setOffset] = useState(0);
  const accountId = accounts.some((account) => account.id === selectedAccount) ? selectedAccount : "";
  const failures = useQuery({
    queryKey: [...queryKey, accountId, offset],
    queryFn: () => listDownloadFailures(accountId, pageSize, offset),
    refetchInterval: 30_000,
  });
  const retry = useMutation({
    mutationFn: (account: string) => retryDownloads(account),
    onSettled: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey }),
        ...["messages", "folders", "folder-unread-counts", "account-unread-counts"].map((key) => queryClient.invalidateQueries({ queryKey: [key] })),
      ]);
    },
  });
  useEffect(() => {
    const unlisten = listen<{ status: string }>("mail:sync-progress", ({ payload }) => {
      if (payload.status === "completed" || payload.status === "error") {
        void queryClient.invalidateQueries({ queryKey });
      }
    });
    return () => { void unlisten.then((off) => off()).catch(() => {}); };
  }, [queryClient]);
  const total = failures.data?.total;
  useEffect(() => {
    if (total !== undefined && offset >= total && offset > 0) {
      setOffset(Math.max(0, Math.floor((total - 1) / pageSize) * pageSize));
    }
  }, [total, offset]);

  return (
    <section aria-labelledby="mail-download-failures-title" style={{ marginTop: "24px", color: "var(--color-text-primary)" }}>
      <h3 id="mail-download-failures-title" style={{ fontSize: "14px", fontWeight: 600 }}>{t("downloads.title", "Mail downloads")}</h3>
      <p style={{ color: "var(--color-text-secondary)", fontSize: "12px", lineHeight: 1.5 }}>
        {t("downloads.description", "Messages that could not be downloaded are saved for retry. Other mail continues syncing.")}
      </p>
      <div style={{ display: "flex", alignItems: "center", gap: "10px", flexWrap: "wrap" }}>
        <select aria-label={t("downloads.account", "Download account")} value={accountId} disabled={retry.isPending}
          onChange={(event) => { setSelectedAccount(event.target.value); setOffset(0); retry.reset(); }}
          style={{ ...inputStyle, width: "auto", maxWidth: "100%" }}>
          <option value="">{t("downloads.allAccounts", "All Gmail accounts")}</option>
          {accounts.map((account) => <option key={account.id} value={account.id}>{account.account_label || account.email}</option>)}
        </select>
        <button type="button" style={buttonStyle} disabled={retry.isPending || !total} onClick={() => retry.mutate(accountId)}>
          {retry.isPending ? t("downloads.retrying", "Retrying…") : t("downloads.retry", "Retry failed downloads")}
        </button>
      </div>
      {failures.isPending && <p role="status" style={{ fontSize: "12px" }}>{t("common.loading", "Loading...")}</p>}
      {failures.isError && <div role="alert" style={{ marginTop: "10px" }}>
        {t("downloads.loadFailed", "Could not load failed downloads.")}
        <button type="button" style={{ ...buttonStyle, marginLeft: "8px" }} onClick={() => void failures.refetch()}>{t("common.retry", "Retry")}</button>
      </div>}
      {retry.isError && <p role="alert" style={{ fontSize: "12px" }}>{t("downloads.retryFailed", "Could not retry downloads. Check your connection and account sign-in, then try again.")}</p>}
      {retry.isSuccess && <p role="status" style={{ fontSize: "12px", color: "var(--color-text-secondary)" }}>{t("downloads.retryDone", "Retry finished.")}</p>}
      {failures.data && <>
        <p role="status" style={{ fontSize: "12px" }}>{total === 0 ? t("downloads.empty", "No failed downloads") : t("downloads.pending", "{{count}} messages waiting for download", { count: total })}</p>
        <ul style={{ listStyle: "none", padding: 0, margin: 0 }}>
          {failures.data.items.map((failure) => <li key={`${failure.account_id}:${failure.remote_id}`}
            style={{ padding: "12px 0", borderBottom: "1px solid var(--color-border)", fontSize: "12px", overflowWrap: "anywhere" }}>
            <div style={{ fontWeight: 500 }}>{failure.subject || t("downloads.unavailableSubject", "Message details unavailable")}</div>
            <div style={{ color: "var(--color-text-secondary)", marginTop: "4px" }}>{failure.account_email}</div>
            <div style={{ marginTop: "4px" }}>{failure.kind === "too_large"
              ? t("downloads.tooLarge", "Message exceeds the 50 MiB download limit.")
              : t("downloads.invalidContent", "The message could not be decoded.")}</div>
            <div style={{ color: "var(--color-text-secondary)", marginTop: "4px" }}>
              {t("downloads.attempts", "Attempts: {{count}}", { count: failure.attempts })} · {failure.next_retry_at === 0
                ? t("downloads.queued", "Queued for retry")
                : t("downloads.nextRetry", "Eligible for retry: {{time}}", { time: new Date(failure.next_retry_at * 1000).toLocaleString() })}
            </div>
          </li>)}
        </ul>
        {(offset > 0 || (total ?? 0) > pageSize) && <div style={{ display: "flex", gap: "10px", marginTop: "12px" }}>
          <button type="button" style={buttonStyle} disabled={offset === 0 || retry.isPending} onClick={() => setOffset(Math.max(0, offset - pageSize))}>{t("downloads.previous", "Previous")}</button>
          <button type="button" style={buttonStyle} disabled={offset + pageSize >= (total ?? 0) || retry.isPending} onClick={() => setOffset(offset + pageSize)}>{t("downloads.next", "Next")}</button>
        </div>}
      </>}
    </section>
  );
}
