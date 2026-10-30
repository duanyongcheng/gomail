import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { listen } from "@tauri-apps/api/event";
import { deleteDraft } from "@/lib/api";
import {
  listLocalDrafts, getLocalDraft, changeCloudDraft, listGmailDrafts, importGmailDraft,
  type CloudDraftAction, type DraftSummary, type RemoteDraftSummary,
} from "@/lib/platform/compose";
import { extractErrorMessage } from "@/lib/extractErrorMessage";
import { useComposeStore } from "@/stores/compose.store";
import { useConfirmStore } from "@/stores/confirm.store";

const actionStyle = {
  padding: "4px 9px", border: "1px solid var(--color-border)", borderRadius: 6,
  background: "var(--color-bg-primary)", color: "var(--color-text-primary)",
  fontSize: 12, whiteSpace: "nowrap" as const,
};
const rowStyle = {
  display: "flex", flexWrap: "wrap" as const, alignItems: "center", gap: "8px 12px",
  padding: "10px 0", borderTop: "1px solid var(--color-border)",
};

export default function LocalDrafts({ accountId, disabled, initialGmailAccountId }: { accountId: string; disabled: boolean; initialGmailAccountId?: string }) {
  const { t } = useTranslation();
  const [panel, setPanel] = useState<"local" | "gmail" | null>(null);
  const [version, setVersion] = useState(0);
  const [items, setItems] = useState<DraftSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);
  const [busy, setBusy] = useState(false);
  const [cloudError, setCloudError] = useState<{ account: string; message: string } | null>(null);
  const [notice, setNotice] = useState<{ account: string; message: string } | null>(null);
  const [remote, setRemote] = useState<{ account: string; items: RemoteDraftSummary[]; next: string }>({ account: "", items: [], next: "" });
  const [remoteLoading, setRemoteLoading] = useState(false);
  const [remoteError, setRemoteError] = useState<{ account: string; token: string } | null>(null);
  const currentAccount = useRef(accountId);
  currentAccount.current = accountId;
  const currentlyDisabled = useRef(disabled);
  currentlyDisabled.current = disabled;
  const alive = useRef(true);
  const action = useRef(false);
  const remoteRequest = useRef(0);
  const remoteBusy = useRef(false);
  const initialBrowserOpened = useRef(false);
  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; remoteRequest.current++; };
  }, []);
  useEffect(() => {
    remoteRequest.current++;
    remoteBusy.current = false;
    setRemoteLoading(false);
    setRemoteError(null);
    // An account change does not authorize a new Gmail request.
    setPanel((previous) => previous === "gmail" ? null : previous);
  }, [accountId]);
  useEffect(() => {
    if (panel !== "local" || !accountId) return;
    let cancelled = false;
    let request = 0;
    const reload = () => {
      if (cancelled || currentAccount.current !== accountId) return;
      const latest = ++request;
      const isCurrent = () => !cancelled && currentAccount.current === accountId && latest === request;
      setLoading(true); setError(false);
      void listLocalDrafts(accountId).then((rows) => { if (isCurrent()) setItems(rows); })
        .catch(() => { if (isCurrent()) setError(true); })
        .finally(() => { if (isCurrent()) setLoading(false); });
    };
    // The native event is global. Reload only this open account's local rows;
    // cancellation unlocks a retained draft, acceptance removes it.
    const unlisten = listen("mail:outbox-changed", reload);
    void unlisten.catch(() => { if (!cancelled) setError(true); });
    reload();
    return () => {
      cancelled = true;
      void unlisten.then((off) => off(), () => {});
    };
  }, [panel, accountId, version]);

  const stillCurrent = (account: string) => alive.current && currentAccount.current === account;
  const canReplace = (account: string) => stillCurrent(account) && !currentlyDisabled.current;
  const title = (subject: string) => subject || t("compose.untitledDraft", "Untitled draft");

  function finishAction(account: string) {
    action.current = false;
    if (alive.current) setBusy(false);
    if (stillCurrent(account)) setVersion((v) => v + 1);
  }
  async function restore(item: DraftSummary) {
    if (!canReplace(item.accountId) || action.current || item.cloudState === "sending" || item.cloudState === "sent") return;
    action.current = true; setBusy(true); setError(false);
    try {
      const draft = await getLocalDraft(item.accountId, item.id);
      if (canReplace(item.accountId)) useComposeStore.getState().openCompose("new", null, { localDraft: draft });
    } catch { if (stillCurrent(item.accountId)) setError(true); }
    finally { finishAction(item.accountId); }
  }
  async function remove(item: DraftSummary) {
    if (action.current || !canReplace(item.accountId)) return;
    action.current = true; setBusy(true); setError(false);
    try {
      const confirmed = await useConfirmStore.getState().confirm({
        title: t("compose.deleteLocalDraft", "Delete local draft?"),
        message: `${title(item.subject)}\n${t("cloudDrafts.localDelete", "Only the local draft will be removed. Any Gmail copy remains in Gmail.")}`,
        confirmLabel: t("common.delete", "Delete"),
        destructive: true,
      });
      if (!confirmed || !canReplace(item.accountId)) return;
      await deleteDraft(item.accountId, item.id);
    } catch { if (stillCurrent(item.accountId)) setError(true); }
    finally { finishAction(item.accountId); }
  }
  async function cloud(item: DraftSummary, operation: CloudDraftAction) {
    if (action.current || !canReplace(item.accountId)) return;
    action.current = true; setBusy(true); setCloudError(null); setNotice(null);
    try {
      if (operation === "delete" || operation === "keep-local" || operation === "import-remote") {
        const copy = operation === "delete"
          ? {
            title: t("cloudDrafts.deleteTitle", "Delete Gmail draft permanently?"),
            message: t("cloudDrafts.deleteWarning", "This permanently removes the checked Gmail draft, not just this device's copy. It cannot be undone. Your local draft is kept."),
            confirmLabel: t("cloudDrafts.delete", "Delete Gmail copy"),
          }
          : operation === "keep-local"
            ? {
              title: t("cloudDrafts.detachTitle", "Keep an independent local draft?"),
              message: t("cloudDrafts.detachWarning", "Your local content is kept and its Gmail link is removed. Gmail is not changed. Uploading later creates a separate Gmail draft."),
              confirmLabel: t("cloudDrafts.keepLocal", "Keep local version"),
            }
            : {
              title: t("cloudDrafts.keepBothTitle", "Keep both draft versions?"),
              message: t("cloudDrafts.keepBothWarning", "Keep your local content as an independent draft and import the current Gmail version as a separate local draft. Nothing in Gmail is overwritten."),
              confirmLabel: t("cloudDrafts.keepBoth", "Keep both versions"),
            };
        const confirmed = await useConfirmStore.getState().confirm({ ...copy, destructive: operation === "delete", message: `${title(item.subject)}\n${copy.message}` });
        if (!confirmed || !canReplace(item.accountId)) return;
      }
      const result = await changeCloudDraft(item.accountId, item.id, operation);
      if (operation === "import-remote" && result.draftId && canReplace(item.accountId)) {
        const draft = await getLocalDraft(item.accountId, result.draftId);
        if (canReplace(item.accountId)) useComposeStore.getState().openCompose("new", null, { localDraft: draft });
      }
    } catch (err) {
      if (stillCurrent(item.accountId)) setCloudError({ account: item.accountId, message: extractErrorMessage(err) });
    } finally { finishAction(item.accountId); }
  }
  async function loadRemote(token = "") {
    if (!accountId || remoteBusy.current || action.current) return;
    const account = accountId;
    const request = ++remoteRequest.current;
    remoteBusy.current = true; setRemoteLoading(true); setRemoteError(null); setPanel("gmail");
    if (!token) setRemote({ account, items: [], next: "" });
    try {
      const page = await listGmailDrafts(account, token);
      if (!stillCurrent(account) || remoteRequest.current !== request) return;
      setRemote((previous) => {
        const merged = token && previous.account === account ? [...previous.items, ...page.items] : page.items;
        return { account, items: [...new Map(merged.map((item) => [item.id, item])).values()], next: page.nextPageToken };
      });
    } catch {
      if (stillCurrent(account) && remoteRequest.current === request) setRemoteError({ account, token });
    } finally {
      if (remoteRequest.current === request) {
        remoteBusy.current = false;
        if (alive.current) setRemoteLoading(false);
      }
    }
  }
  async function importRemote(item: RemoteDraftSummary) {
    const account = remote.account;
    if (action.current || !canReplace(account)) return;
    action.current = true; setBusy(true); setCloudError(null); setNotice(null);
    try {
      const draft = item.localDraftId
        ? await getLocalDraft(account, item.localDraftId)
        : await importGmailDraft(account, item.id, item.messageId);
      if (canReplace(account)) {
        useComposeStore.getState().openCompose("new", null, { localDraft: draft });
      } else if (stillCurrent(account)) {
        setNotice({ account, message: t("cloudDrafts.importKept", "The imported draft is saved in Local drafts. Your current edits were not replaced.") });
      }
    } catch (err) {
      if (stillCurrent(account)) setCloudError({ account, message: extractErrorMessage(err) });
    } finally { finishAction(account); }
  }
  function showLocal() {
    if (busy) return;
    remoteRequest.current++;
    remoteBusy.current = false;
    setRemoteLoading(false);
    setPanel(panel === "local" ? null : "local");
  }
  const remoteVisible = panel === "gmail" && remote.account === accountId;
  useEffect(() => {
    if (initialBrowserOpened.current || !initialGmailAccountId || initialGmailAccountId !== accountId) return;
    let cancelled = false;
    // This prefill comes only from the explicit Drafts-folder management action.
    // Defer one microtask so StrictMode's rehearsal does not duplicate the read.
    void Promise.resolve().then(() => {
      if (cancelled || !alive.current || currentAccount.current !== initialGmailAccountId) return;
      initialBrowserOpened.current = true;
      void loadRemote();
    });
    return () => { cancelled = true; };
  }, [initialGmailAccountId, accountId]); // eslint-disable-line react-hooks/exhaustive-deps

  return <section style={{ padding: "8px 20px", borderBottom: "1px solid var(--color-border)", fontSize: 13 }}>
    <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
      <button type="button" style={actionStyle} onClick={showLocal} aria-expanded={panel === "local"} disabled={!accountId || busy}>
        {t("compose.localDrafts", "Local drafts")}
      </button>
      <button type="button" style={actionStyle} aria-expanded={remoteVisible} disabled={!accountId || busy || remoteLoading}
        onClick={() => remoteVisible ? setPanel(null) : void loadRemote()}>
        {t("cloudDrafts.browse", "Gmail drafts")}
      </button>
    </div>
    {panel && <div>
      <p style={{ margin: "8px 0", color: "var(--color-text-secondary)", lineHeight: 1.5 }}>{t("cloudDrafts.notice", "Autosave is encrypted on this device only; not synced to Gmail automatically. Upload saved changes explicitly. Sending a linked draft consumes its Gmail copy only after Gmail accepts the send. Do not edit the same draft in Gmail and Pebble at the same time.")}</p>
      {cloudError?.account === accountId && <p role="alert">{cloudError.message}</p>}
      {notice?.account === accountId && <p role="status">{notice.message}</p>}
      {disabled && <p>{t("compose.saveBeforeRestore", "Save and close or discard this message before opening another draft.")}</p>}
      {panel === "local" && (loading ? <p role="status">{t("common.loading", "Loading...")}</p> : error ? <p role="alert">
        {t("compose.localDraftError", "Could not load or change local drafts.")}
        <button onClick={() => setVersion((v) => v + 1)}>{t("common.retry", "Retry")}</button>
      </p> : items.filter((item) => item.accountId === accountId).length === 0 ? <p>{t("compose.noLocalDrafts", "No local drafts")}</p> : <ul style={{ listStyle: "none", padding: 0, margin: "8px 0" }}>
        {items.filter((item) => item.accountId === accountId).map((item) => {
          const unresolved = ["unknown", "deleting", "sending"].includes(item.cloudState);
          const sent = item.cloudState === "sent";
          const uploadAllowed = ["local", "synced", "changed"].includes(item.cloudState);
          return <li key={item.id} data-local-draft-id={item.id} style={rowStyle}>
            <button style={{ ...actionStyle, fontSize: 13, maxWidth: "100%", overflow: "hidden", textOverflow: "ellipsis" }} disabled={busy || disabled || item.cloudState === "sending" || sent} onClick={() => void restore(item)}>{title(item.subject)}</button>
            <span style={{ color: "var(--color-text-secondary)", fontSize: 12 }}>{new Date(item.updatedAt * 1000).toLocaleString()}</span>
            {item.cloudAvailable && <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 8 }}>
              <span style={{ color: "var(--color-text-secondary)", fontSize: 12, marginRight: 4 }}>{t(`cloudDrafts.state.${item.cloudState}`, item.cloudState)}</span>
              {!sent && item.cloudState !== "sending" && <>
                <button style={actionStyle} disabled={busy || disabled || !uploadAllowed} onClick={() => void cloud(item, "upload")}>
                  {item.cloudState === "local" ? t("cloudDrafts.upload", "Upload to Gmail") : t("cloudDrafts.update", "Update Gmail copy")}
                </button>
                {item.cloudState !== "local" && <button style={actionStyle} disabled={busy || disabled} onClick={() => void cloud(item, "check")}>{t("cloudDrafts.check", "Check Gmail copy")}</button>}
                {["synced", "changed"].includes(item.cloudState) && <button style={actionStyle} disabled={busy || disabled} onClick={() => void cloud(item, "delete")}>{t("cloudDrafts.delete", "Delete Gmail copy")}</button>}
                {item.cloudState === "conflict" && <>
                  <button style={actionStyle} disabled={busy || disabled} onClick={() => void cloud(item, "keep-local")}>{t("cloudDrafts.keepLocal", "Keep local version")}</button>
                  <button style={actionStyle} disabled={busy || disabled} onClick={() => void cloud(item, "import-remote")}>{t("cloudDrafts.keepBoth", "Keep both versions")}</button>
                </>}
              </>}
              {item.cloudState === "sending" && <span>{t("cloudDrafts.outboxHint", "Check this send in Outbox. Do not send another copy while its outcome is unknown.")}</span>}
              {sent && <span>{t("cloudDrafts.sentHint", "Gmail accepted this draft's send. Start a new compose for another message.")}</span>}
            </div>}
            <button style={actionStyle} disabled={busy || disabled || unresolved} onClick={() => void remove(item)} aria-label={`${t("common.delete", "Delete")}: ${item.subject}`}>{t("common.delete", "Delete")}</button>
          </li>;
        })}
      </ul>)}
      {remoteVisible && <div>
        <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
          <button style={actionStyle} disabled={busy || remoteLoading} onClick={() => void loadRemote()}>{t("cloudDrafts.refresh", "Refresh Gmail drafts")}</button>
          <span style={{ color: "var(--color-text-secondary)" }}>{t("cloudDrafts.importNotice", "Import a Gmail draft to edit it locally. Browsing and importing do not change Gmail.")}</span>
        </div>
        {remote.items.length > 0 && <ul style={{ listStyle: "none", padding: 0, margin: "8px 0" }}>
          {remote.items.map((item) => <li key={item.id} data-remote-draft-id={item.id} style={rowStyle}>
            <span style={{ flex: "1 1 180px", overflowWrap: "anywhere" }}>{title(item.subject)}</span>
            {item.updatedAt > 0 && <span style={{ color: "var(--color-text-secondary)", fontSize: 12 }}>{new Date(item.updatedAt * 1000).toLocaleString()}</span>}
            <button style={actionStyle} disabled={busy || disabled || remoteLoading} onClick={() => void importRemote(item)}
              aria-label={`${item.localDraftId ? t("cloudDrafts.openLocal", "Open local draft") : t("cloudDrafts.import", "Import draft")}: ${title(item.subject)}`}>
              {item.localDraftId ? t("cloudDrafts.openLocal", "Open local draft") : t("cloudDrafts.import", "Import draft")}
            </button>
          </li>)}
        </ul>}
        {remoteLoading && <p role="status">{t("common.loading", "Loading...")}</p>}
        {remoteError?.account === accountId && <p role="alert">
          {t("cloudDrafts.listError", "Could not load Gmail drafts. Your local drafts are unchanged.")}
          <button style={actionStyle} disabled={busy || remoteLoading} onClick={() => void loadRemote(remoteError.token)}>{t("common.retry", "Retry")}</button>
        </p>}
        {!remoteLoading && !remoteError && remote.items.length === 0 && <p>{t("cloudDrafts.empty", "No Gmail drafts")}</p>}
        {remote.next && !remoteError && <button style={actionStyle} disabled={busy || remoteLoading} onClick={() => void loadRemote(remote.next)}>{t("cloudDrafts.more", "Load more drafts")}</button>}
      </div>}
    </div>}
  </section>;
}
