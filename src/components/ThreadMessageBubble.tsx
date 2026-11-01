import { useState, useEffect, useRef } from "react";
import { useUpdateFlagsMutation } from "@/hooks/mutations/useUpdateFlagsMutation";
import { messageFlagsMutationKey } from "@/hooks/mutations/messageFlagsMutation";
import { useQueryClient } from "@tanstack/react-query";
import { useMessageQuery } from "@/hooks/queries/useMessageQuery";
import { manualUnreadVersion } from "@/hooks/queries/messageFlagsCache";
import { useComposeStore } from "@/stores/compose.store";
import AttachmentList from "./AttachmentList";
import { ChevronDown, ChevronRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { getRenderedHtml, getMessage } from "@/lib/api";
import type { Message, RenderedHtml, PrivacyMode } from "@/lib/api";
import { defaultPrivacyMode } from "@/lib/privacyMode";
import { subscribePrivacyChanges } from "@/lib/privacyEvents";
import { useSenderTrust } from "@/hooks/useSenderTrust";
import PrivacyBanner from "./PrivacyBanner";
import { sanitizeHtml } from "@/lib/sanitizeHtml";
import { ShadowDomEmail } from "./ShadowDomEmail";
import ContactAddressAction from "./ContactAddressAction";
import { uniqueContactParticipants } from "./contact-participants";
import { messageThemeVariables, type MessageTheme } from "@/lib/messageThemes";

interface Props {
  message: Message;
  defaultExpanded?: boolean;
  theme?: MessageTheme;
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

function ThreadBubble({ message, defaultExpanded = false, theme }: Props) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(defaultExpanded);
  const client = useQueryClient();
  // Metadata is local and independent of the lazy body renderer. Re-enabling
  // this staleTime=0 query also refreshes a collapsed bubble on re-expansion.
  const metadata = useMessageQuery(expanded ? message.id : null);
  const openedUnreadVersion = useRef(manualUnreadVersion(client, message.id));
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
  const flagsMutation = useUpdateFlagsMutation();
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
    if (!expanded) {
      readMarked.current = false;
      openedUnreadVersion.current = manualUnreadVersion(client, message.id);
      return;
    }
    if (readMarked.current) return;
    if (openedUnreadVersion.current !== manualUnreadVersion(client, message.id)) {
      readMarked.current = true;
      return;
    }
    const flags = metadata.data ?? message;
    // Cached unread can be acknowledged immediately, cancelling older reads.
    // Cached read (or read-only capability) cannot consume this opening until
    // the fresh local query has answered. Never wait for Rendered/HTML here.
    if ((flags.is_read || flags.capabilities?.modify_flags === false)
      && (metadata.isFetching || !metadata.data || metadata.isError
        || client.isMutating({ mutationKey: messageFlagsMutationKey }) > 0)) return;
    readMarked.current = true;
    if (flags.is_read || flags.capabilities?.modify_flags === false) return;
    flagsMutation.mutate({ messageId: message.id, isRead: true }, {
      onError: () => { if (alive.current) setActionError(true); },
    });
  }, [expanded, message.id, message.is_read, message.capabilities?.modify_flags, metadata.data, metadata.isFetching, metadata.isError, retry]); // eslint-disable-line react-hooks/exhaustive-deps

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
      className="thread-message-bubble"
      data-expanded={expanded}
      style={theme ? messageThemeVariables(theme) : undefined}
    >
      {/* Header - always visible */}
      <button
        className="thread-message-bubble-toggle"
        onClick={() => {
          if (!expanded) openedUnreadVersion.current = manualUnreadVersion(client, message.id);
          setExpanded(!expanded);
        }}
        aria-expanded={expanded}
      >
        {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        <span style={{ fontSize: "13px", fontWeight: 500, flex: 1 }}>
          {message.from_name || message.from_address}
        </span>
        <span className="thread-message-bubble-date">
          {formatFullDate(message.date)}
        </span>
      </button>

      {/* Body - only when expanded */}
      {expanded && (
        <div className="thread-message-bubble-content">
          {/* To/Cc line */}
          <div style={{ fontSize: "12px", color: "var(--msg-meta-color, var(--color-text-secondary))", marginBottom: "8px" }}>
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
          {actionError && <p role="alert">{t("thread.actionFailed", "Could not update or open this message.")} <button onClick={() => { readMarked.current = false; setActionError(false); setRetry((v) => v + 1); }}>{t("common.retry", "Retry")}</button></p>}
          {rendered && <PrivacyBanner rendered={rendered} onLoadImages={() => setMode("LoadOnce")} onTrustSender={(type) => void trust.save(type)} busy={trust.busy} error={trust.error} />}
          {trust.error && !rendered && <p role="alert">{trust.error}</p>}
          {/* Body content */}
          {rendered?.html ? (
            <ShadowDomEmail html={rendered.html} theme={theme} />
          ) : (
            <pre style={{
              fontSize: "var(--msg-body-size, 13px)", color: "var(--msg-body-color, var(--color-text-primary))",
              whiteSpace: "pre-wrap", wordBreak: "break-word",
              margin: 0, fontFamily: "var(--msg-body-font, inherit)", lineHeight: "var(--msg-body-line-height, 1.8)",
            }}>
              {message.body_text}
            </pre>
          )}
          <div className="thread-message-bubble-actions">
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
