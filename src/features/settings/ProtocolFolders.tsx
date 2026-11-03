import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { FolderService } from "@/mygo";
import type { FolderManagementView } from "@/mygo";
import { useConfirmStore } from "@/stores/confirm.store";
import { extractErrorMessage } from "@/lib/extractErrorMessage";

export default function ProtocolFolders({ accountId, onBusy }: { accountId: string; onBusy?: (busy: boolean) => void }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const [view, setView] = useState<FolderManagementView | null>(null);
  const [name, setName] = useState("");
  const [folderId, setFolderId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loadFailed, setLoadFailed] = useState(false);
  const lock = useRef(false);
  const alive = useRef(true);
  useEffect(() => {
    let active = true; alive.current = true;
    void FolderService.management({ accountId }).then((result) => { if (active) setView(result); }).catch(() => { if (active) setLoadFailed(true); });
    return () => { active = false; alive.current = false; };
  }, [accountId]);
  async function act(action: string) {
    if (lock.current || !view) return;
    lock.current = true; setBusy(true); onBusy?.(true); setError("");
    try {
      if (action === "delete" && !await useConfirmStore.getState().confirm({ title: t("protocolFolders.deleteTitle", "Delete server folder"), message: t("protocolFolders.deleteWarning", "Delete this folder on the mail server? Messages in it may be removed by the server. This cannot be undone here."), destructive: true })) return;
      if (action === "dismiss" && !await useConfirmStore.getState().confirm({ title: t("protocolFolders.dismissTitle", "Dismiss unresolved folder action"), message: t("protocolFolders.dismissWarning", "This clears the local action record without retrying it. Check the server folder state first; an uncertain operation may already have succeeded."), destructive: true })) return;
      const result = await FolderService.manage({ accountId, action, folderId, name, pendingId: view.pending?.id ?? "" });
      if (alive.current) { setView(result); setName(""); }
      await client.invalidateQueries({ queryKey: ["folders"] });
    } catch (err) { if (alive.current) setError(extractErrorMessage(err)); }
    finally { lock.current = false; if (alive.current) { setBusy(false); onBusy?.(false); } }
  }
  const actionLabel = view?.pending?.action === "create" ? t("protocolFolders.create", "Create folder")
    : view?.pending?.action === "rename" ? t("protocolFolders.rename", "Rename folder")
    : view?.pending?.action === "delete" ? t("protocolFolders.delete", "Delete folder")
    : t("protocolFolders.action", "Folder change");
  return <div><fieldset disabled={busy || !view} style={{ display: "grid", gap: 8 }}><legend>{t("protocolFolders.title", "Server folder management")}</legend>
    <button type="button" onClick={() => void act("refresh")}>{t("protocolFolders.refresh", "Refresh server folders")}</button>
    {view?.pending && <div role="status">{t("protocolFolders.unresolved", `A folder operation is unresolved (${actionLabel}). Refresh and inspect server state before continuing.`, { action: actionLabel })}<button type="button" onClick={() => void act("dismiss")}>{t("protocolFolders.dismiss", "Dismiss unresolved action")}</button></div>}
    <label>{t("protocolFolders.folder", "Folder")} <select value={folderId} onChange={(e) => setFolderId(e.target.value)}><option value="">{t("protocolFolders.choose", "Choose folder")}</option>{view?.folders.filter((folder) => !folder.is_system).map((folder) => <option value={folder.id} key={folder.id}>{folder.name}</option>)}</select></label>
    <label>{t("protocolFolders.newName", "New folder name")} <input value={name} onChange={(e) => setName(e.target.value)} maxLength={225} /></label>
    <div><button type="button" disabled={!!view?.pending || !name.trim()} onClick={() => void act("create")}>{t("protocolFolders.create", "Create folder")}</button> <button type="button" disabled={!!view?.pending || !folderId || !name.trim()} onClick={() => void act("rename")}>{t("protocolFolders.rename", "Rename folder")}</button> <button type="button" disabled={!!view?.pending || !folderId} onClick={() => void act("delete")}>{t("protocolFolders.delete", "Delete folder")}</button></div>
  </fieldset>
    {loadFailed && <p role="alert">{t("protocolFolders.loadFailed", "Could not load folder actions.")}</p>}
    {error && <p role="alert">{error}</p>}
  </div>;
}
