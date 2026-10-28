import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { deleteDraft } from "@/lib/api";
import { listLocalDrafts, getLocalDraft, type DraftSummary } from "@/lib/platform/compose";
import { useComposeStore } from "@/stores/compose.store";
import { useConfirmStore } from "@/stores/confirm.store";

export default function LocalDrafts({ accountId, disabled }: { accountId: string; disabled: boolean }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [version, setVersion] = useState(0);
  const [items, setItems] = useState<DraftSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);
  const [busy, setBusy] = useState(false);
  const currentAccount = useRef(accountId);
  currentAccount.current = accountId;
  const alive = useRef(true);
  const action = useRef(false);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  useEffect(() => {
    if (!open || !accountId) return;
    let cancelled = false;
    setLoading(true); setError(false);
    listLocalDrafts(accountId).then((rows) => { if (!cancelled) setItems(rows); })
      .catch(() => { if (!cancelled) setError(true); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [open, accountId, version]);

  async function restore(item: DraftSummary) {
    if (disabled || action.current) return;
    action.current = true; setBusy(true); setError(false);
    try {
      const draft = await getLocalDraft(item.accountId, item.id);
      if (alive.current && currentAccount.current === item.accountId) {
        useComposeStore.getState().openCompose("new", null, { localDraft: draft });
      }
    } catch { if (alive.current) setError(true); } finally { action.current = false; if (alive.current) setBusy(false); }
  }
  async function remove(item: DraftSummary) {
    if (action.current || disabled) return;
    action.current = true; setBusy(true); setError(false);
    try {
      const confirmed = await useConfirmStore.getState().confirm({
        title: t("compose.deleteLocalDraft", "Delete local draft?"),
        message: item.subject || t("compose.untitledDraft", "Untitled draft"),
        confirmLabel: t("common.delete", "Delete"),
      });
      if (!confirmed || !alive.current || currentAccount.current !== item.accountId) return;
      await deleteDraft(item.accountId, item.id);
      if (alive.current) setVersion((v) => v + 1);
    } catch { if (alive.current) setError(true); } finally { action.current = false; if (alive.current) setBusy(false); }
  }
  return <section style={{ padding: "8px 20px", borderBottom: "1px solid var(--color-border)", fontSize: 13 }}>
    <button type="button" onClick={() => setOpen(!open)} aria-expanded={open} disabled={!accountId || busy}>
      {t("compose.localDrafts", "Local drafts")}
    </button>
    {open && <div>
      <p>{t("compose.localDraftsNotice", "Encrypted on this device only; not synced to Gmail.")}</p>
      {disabled && <p>{t("compose.saveBeforeRestore", "Save and close or discard this message before opening another draft.")}</p>}
      {loading ? <p role="status">{t("common.loading", "Loading...")}</p> : error ? <p role="alert">
        {t("compose.localDraftError", "Could not load or change local drafts.")}
        <button onClick={() => setVersion((v) => v + 1)}>{t("common.retry", "Retry")}</button>
      </p> : items.length === 0 ? <p>{t("compose.noLocalDrafts", "No local drafts")}</p> : <ul>
        {items.map((item) => <li key={item.id} style={{ display: "flex", gap: 12, padding: 4 }}>
          <button disabled={busy || disabled} onClick={() => void restore(item)}>{item.subject || t("compose.untitledDraft", "Untitled draft")}</button>
          <span>{new Date(item.updatedAt * 1000).toLocaleString()}</span>
          <button disabled={busy || disabled} onClick={() => void remove(item)} aria-label={`${t("common.delete", "Delete")}: ${item.subject}`}>{t("common.delete", "Delete")}</button>
        </li>)}
      </ul>}
    </div>}
  </section>;
}
