import { useState, useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useComposeStore } from "@/stores/compose.store";
import AttachmentList from "./AttachmentList";
import { ChevronDown, ChevronRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { getRenderedHtml, getMessage, updateMessageFlags } from "@/lib/api";
import type { Message, RenderedHtml, PrivacyMode } from "@/lib/api";
import { defaultPrivacyMode } from "@/lib/privacyMode";
import { subscribePrivacyChanges } from "@/lib/privacyEvents";
import { useSenderTrust } from "@/hooks/useSenderTrust";
import PrivacyBanner from "./PrivacyBanner";
import { sanitizeHtml } from "@/lib/sanitizeHtml";
import { ShadowDomEmail } from "./ShadowDomEmail";
import ContactAddressAction from "./ContactAddressAction";
import { uniqueContactParticipants } from "./contact-participants";

interface Props {
  message: Message;
  defaultExpanded?: boolean;
}

function formatFullDate(timestamp: number): string {
  return new Date(timestamp * 1000).toLocaleString([], {
    year: "numeric", month: "short", day: "numeric",
    hour: "2-digit", minute: "2-digit",
  });
}

export default function ThreadMessageBubble(props: Props) {
  return <ThreadBubble key={props.message.id} {...props} />;
}

function ThreadBubble({ message, defaultExpanded = false }: Props) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(defaultExpanded);
  const [rendered, setRendered] = useState<RenderedHtml | null>(null);
  const [mode, setMode] = useState<PrivacyMode>(() => defaultPrivacyMode());
  const generation = useRef(0);
  const [renderError, setRenderError] = useState(false);
  const [renderLoading, setRenderLoading] = useState(false);
  const [actionError, setActionError] = useState(false);
  const [retry, setRetry] = useState(0);
  const [busy, setBusy] = useState(false);
  const actionBusy = useRef(false);
  const readMarked = useRef(false);
  const alive = useRef(true);
  const client = useQueryClient();
  const refreshPrivacy = () => { generation.current++; setRendered(null); setRetry((v) => v + 1); };
  const trust = useSenderTrust(message, refreshPrivacy);
  useEffect(() => subscribePrivacyChanges((change) => {
    if (change.settings) setMode(defaultPrivacyMode());
    if (change.settings || (change.accountId === message.account_id && (!change.email || change.email.toLowerCase() === message.from_address.trim().toLowerCase()))) refreshPrivacy();
  }), [message.account_id, message.from_address]);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const contactParticipants = uniqueContactParticipants(
    { name: message.from_name, address: message.from_address },
    message.to_list,
    message.cc_list,
  );

  useEffect(() => {
    if (!expanded) return;
    let cancelled = false;
    const started = ++generation.current;
    setRendered(null); setRenderError(false); setRenderLoading(true);
    getRenderedHtml(message.id, mode)
      .then((html) => { if (!cancelled && started === generation.current) setRendered({ ...html, html: sanitizeHtml(html.html) }); })
      .catch(() => { if (!cancelled && started === generation.current) setRenderError(true); })
      .finally(() => { if (!cancelled && started === generation.current) setRenderLoading(false); });
    return () => { cancelled = true; };
  }, [expanded, message.id, message.updated_at, retry, mode]);

  useEffect(() => {
    if (message.is_read) readMarked.current = false;
    if (!expanded || message.is_read || message.capabilities?.modify_flags === false || readMarked.current) return;
    readMarked.current = true;
    updateMessageFlags(message.id, true).then(() => {
      for (const key of ["threads", "threadMessages", "messages", "folder-unread-counts", "account-unread-counts"]) void client.invalidateQueries({ queryKey: [key] });
    }).catch(() => { readMarked.current = false; if (alive.current) setActionError(true); });
  }, [expanded, message.id, message.is_read, message.capabilities?.modify_flags, client, retry]);

  async function compose(mode: "reply" | "reply-all" | "forward") {
    if (actionBusy.current) return;
    actionBusy.current = true; setBusy(true); setActionError(false);
    try {
      // Thread list messages are metadata-only. Quotes need the full message.
      const full = await getMessage(message.id);
      if (alive.current) useComposeStore.getState().openCompose(mode, full);
    } catch { if (alive.current) setActionError(true); }
    finally { actionBusy.current = false; if (alive.current) setBusy(false); }
  }

  return (
    <div
      style={{
        border: "1px solid var(--color-border)",
        borderRadius: "8px",
        marginBottom: "8px",
        overflow: "hidden",
        backgroundColor: "var(--color-bg)",
      }}
    >
      {/* Header - always visible */}
      <button
        onClick={() => setExpanded(!expanded)}
        aria-expanded={expanded}
        style={{
          display: "flex",
          alignItems: "center",
          gap: "8px",
          padding: "10px 14px",
          cursor: "pointer",
          backgroundColor: expanded ? "var(--color-bg-hover)" : "transparent",
          border: "none",
          width: "100%",
          textAlign: "left",
          color: "inherit",
          font: "inherit",
        }}
      >
        {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        <span style={{ fontSize: "13px", fontWeight: 500, flex: 1 }}>
          {message.from_name || message.from_address}
        </span>
        <span style={{ fontSize: "11px", color: "var(--color-text-secondary)" }}>
          {formatFullDate(message.date)}
        </span>
      </button>

      {/* Body - only when expanded */}
      {expanded && (
        <div style={{ padding: "12px 14px", borderTop: "1px solid var(--color-border)" }}>
          {/* To/Cc line */}
          <div style={{ fontSize: "12px", color: "var(--color-text-secondary)", marginBottom: "8px" }}>
            <div>
              {t("thread.to")} {message.to_list?.map((r: { address: string }) => r.address).join(", ")}
            </div>
            {message.cc_list?.length > 0 && (
              <div>
                {t("thread.cc", "Cc:")} {message.cc_list.map((r: { address: string }) => r.address).join(", ")}
              </div>
            )}
            <div
              aria-label={t("contacts.participantActions", "Contact actions")}
              style={{ display: "flex", alignItems: "center", gap: "3px", marginTop: "5px" }}
            >
              {contactParticipants.map((participant) => (
                <ContactAddressAction
                  key={participant.address.toLowerCase()}
                  accountId={message.account_id}
                  name={participant.name}
                  address={participant.address}
                />
              ))}
            </div>
          </div>
          {renderLoading && !rendered && <p role="status">{t("common.loading", "Loading...")}</p>}
          {renderError && <p role="alert">{t("thread.renderFailed", "Could not render this message.")} <button onClick={() => setRetry((v) => v + 1)}>{t("common.retry", "Retry")}</button></p>}
          {actionError && <p role="alert">{t("thread.actionFailed", "Could not update or open this message.")} <button onClick={() => { setActionError(false); setRetry((v) => v + 1); }}>{t("common.retry", "Retry")}</button></p>}
          {rendered && <PrivacyBanner rendered={rendered} onLoadImages={() => setMode("LoadOnce")} onTrustSender={(type) => void trust.save(type)} busy={trust.busy} error={trust.error} />}
          {trust.error && !rendered && <p role="alert">{trust.error}</p>}
          {/* Body content */}
          {rendered?.html ? (
            <ShadowDomEmail html={rendered.html} />
          ) : (
            <pre style={{
              fontSize: "13px", color: "var(--color-text-primary)",
              whiteSpace: "pre-wrap", wordBreak: "break-word",
              margin: 0, fontFamily: "inherit",
            }}>
              {message.body_text}
            </pre>
          )}
          <div style={{ display: "flex", gap: 12, marginTop: 12 }}>
            {(["reply", "reply-all", "forward"] as const).map((mode) => <button key={mode} disabled={busy} onClick={() => void compose(mode)}>
              {t(mode === "reply-all" ? "messageActions.replyAll" : `messageActions.${mode}`)}
            </button>)}
          </div>
          {message.has_attachments && <AttachmentList messageId={message.id} />}
        </div>
      )}
    </div>
  );
}
