import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { listen } from "@tauri-apps/api/event";
import { GmailLabelService, type GmailLabelRequest } from "@/mygo";
import type { Account } from "@/lib/api";
import { useConfirmStore } from "@/stores/confirm.store";
import { inputStyle } from "@/styles/form";

const buttonStyle = { padding: "6px 10px", border: "1px solid var(--color-border)", borderRadius: 6, background: "var(--color-bg)", color: "var(--color-text-primary)", fontSize: 12, whiteSpace: "nowrap" as const, flexShrink: 0 };

export default function GmailLabels({ accounts }: { accounts: Account[] }) {
  const { t } = useTranslation();
  const [selected, setSelected] = useState("");
  const accountId = accounts.some((a) => a.id === selected) ? selected : accounts[0]?.id ?? "";
  return <section aria-labelledby="gmail-labels-title" style={{ marginTop: 24 }}>
    <h3 id="gmail-labels-title" style={{ fontSize: 14, fontWeight: 600 }}>{t("gmailLabels.title", "Gmail labels")}</h3>
    <p style={{ fontSize: 12, color: "var(--color-text-secondary)" }}>{t("gmailLabels.notice", "Manage Gmail server labels online. Deleting a label does not delete its messages. Local app tags are separate.")}</p>
    <select aria-label={t("gmailLabels.account", "Label account")} style={inputStyle} value={accountId} onChange={(e) => setSelected(e.target.value)}>
      {accounts.map((a) => <option key={a.id} value={a.id}>{a.account_label || a.email}</option>)}
    </select>
    {accountId && <LabelManager key={accountId} accountId={accountId} />}
  </section>;
}

function LabelManager({ accountId }: { accountId: string }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const queryKey = ["gmail-labels", accountId];
  const query = useQuery({
    queryKey,
    queryFn: () => GmailLabelService.list({ accountId }),
    networkMode: "always", // Display the locally cached Gmail labels; refresh remains explicit.
  });
  const [name, setName] = useState("");
  const [editing, setEditing] = useState<string | null>(null);
  const [editName, setEditName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const acting = useRef(false);
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  useEffect(() => {
    const refresh = () => { void client.invalidateQueries({ queryKey: ["gmail-labels", accountId] }); };
    const changed = listen<{ accountId: string }>("mail:folders-changed", ({ payload }) => { if (payload.accountId === accountId) refresh(); });
    const synced = listen<{ account_id: string }>("mail:sync-complete", ({ payload }) => { if (payload.account_id === accountId) refresh(); });
    return () => { void changed.then((off) => off()); void synced.then((off) => off()); };
  }, [client, accountId]);

  async function run(action: string, folderId = "", labelName = "") {
    if (acting.current) return;
    acting.current = true; setBusy(true); setError("");
    try {
      if (action === "delete" || action === "dismiss") {
        const confirmed = await useConfirmStore.getState().confirm({
          title: t(action === "delete" ? "gmailLabels.deleteTitle" : "gmailLabels.resolveTitle", action === "delete" ? "Delete Gmail label?" : "Resolve uncertain label change?"),
          message: action === "delete"
            ? t("gmailLabels.deleteWarning", "Only this label is removed from Gmail messages. No messages are deleted.") + ` (${labelName})`
            : t("gmailLabels.resolveWarning", "Check Gmail first. The previous change may have succeeded. Clearing this record does not undo it; a later retry may create a duplicate."),
          confirmLabel: t(action === "delete" ? "common.delete" : "gmailLabels.resolve", action === "delete" ? "Delete" : "I checked Gmail; clear record"),
        });
        if (!confirmed || !alive.current) return;
      }
      const request: GmailLabelRequest = { accountId, action, folderId, name: labelName, pendingId: query.data?.pending?.id ?? "" };
      const result = await GmailLabelService.change(request);
      client.setQueryData(queryKey, result);
      if (alive.current) { if (action === "create") setName(""); setEditing(null); }
    } catch (err) {
      if (alive.current) setError(err instanceof Error ? err.message : t("gmailLabels.changeFailed", "Label change failed. Refresh before retrying."));
    } finally {
      // Refresh the local state even on an error: the durable unknown marker
      // may have been inserted before a remote response was lost.
      await Promise.all(["gmail-labels", "folders", "messages", "threads", "threadMessages", "folder-unread-counts", "account-unread-counts", "pendingMailOps", "pendingMailOpsList"].map((key) => client.invalidateQueries({ queryKey: [key] })));
      acting.current = false; if (alive.current) setBusy(false);
    }
  }
  const pending = query.data?.pending;
  const blocked = busy || Boolean(pending);
  return <div style={{ fontSize: 12, marginTop: 10 }}>
    <button type="button" style={buttonStyle} disabled={busy} onClick={() => void run("refresh")}>{t("gmailLabels.refresh", "Refresh from Gmail")}</button>
    {query.isLoading && <p role="status">{t("common.loading", "Loading...")}</p>}
    {query.isError && <p role="alert">{t("gmailLabels.loadFailed", "Could not read cached labels.")} <button onClick={() => void query.refetch()}>{t("common.retry", "Retry")}</button></p>}
    {error && <p role="alert">{error}</p>}
    {pending && <div role="status" style={{ margin: "12px 0", padding: 10, border: "1px solid var(--color-border)", borderRadius: 6 }}>
      <p>{t("gmailLabels.uncertain", "A previous label change needs confirmation. Refresh checks its outcome without repeating it.")}</p>
      <p>{t(`gmailLabels.${pending.action}`, pending.action)}: {pending.name}</p>
      <button style={buttonStyle} disabled={busy} onClick={() => void run("dismiss")}>{t("gmailLabels.resolve", "I checked Gmail; clear record")}</button>
    </div>}
    <form onSubmit={(e) => { e.preventDefault(); void run("create", "", name); }} style={{ display: "flex", gap: 8, marginTop: 12 }}>
      <input aria-label={t("gmailLabels.newName", "New label name")} style={inputStyle} value={name} maxLength={225} disabled={blocked} onChange={(e) => setName(e.target.value)} />
      <button type="submit" style={buttonStyle} disabled={blocked || !name.trim()}>{t("gmailLabels.create", "Create")}</button>
    </form>
    {query.data && <ul className="scroll-region" style={{ listStyle: "none", padding: 0, maxHeight: 320, overflowY: "auto" }}>
      {[...query.data.folders].sort((a, b) => Number(a.is_system) - Number(b.is_system) || a.name.localeCompare(b.name)).map((folder) => <li key={folder.id} style={{ display: "flex", gap: 8, alignItems: "center", padding: "8px 0", borderBottom: "1px solid var(--color-border)", flexWrap: "wrap" }}>
        {editing === folder.id ? <>
          <input aria-label={t("gmailLabels.renameName", "Rename label")} style={inputStyle} value={editName} disabled={blocked} maxLength={225} onChange={(e) => setEditName(e.target.value)} />
          <button style={buttonStyle} disabled={blocked || !editName.trim()} onClick={() => void run("rename", folder.id, editName)}>{t("common.save", "Save")}</button>
          <button style={buttonStyle} disabled={busy} onClick={() => setEditing(null)}>{t("common.cancel", "Cancel")}</button>
        </> : <>
          <span style={{ flex: 1, overflowWrap: "anywhere" }}>{folder.name}</span>
          {folder.is_system ? <span>{t("gmailLabels.system", "System")}</span> : <>
            <button style={buttonStyle} disabled={blocked} aria-label={`${t("gmailLabels.rename", "Rename")}: ${folder.name}`} onClick={() => { setEditing(folder.id); setEditName(folder.name); }}>{t("gmailLabels.rename", "Rename")}</button>
            <button style={buttonStyle} disabled={blocked} aria-label={`${t("common.delete", "Delete")}: ${folder.name}`} onClick={() => void run("delete", folder.id, folder.name)}>{t("common.delete", "Delete")}</button>
          </>}
        </>}
      </li>)}
      {query.data.folders.length === 0 && <li>{t("gmailLabels.empty", "No cached labels. Refresh from Gmail to load them.")}</li>}
    </ul>}
  </div>;
}
