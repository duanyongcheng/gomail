import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { getRenderedHtml } from "@/lib/api";
import { useUpdateFlagsMutation } from "@/hooks/mutations/useUpdateFlagsMutation";
import { messageFlagsMutationKey } from "@/hooks/mutations/messageFlagsMutation";
import { useMessageQuery, messageQueryKey } from "@/hooks/queries/useMessageQuery";
import { manualUnreadVersion } from "@/hooks/queries/messageFlagsCache";
import { sanitizeHtml } from "@/lib/sanitizeHtml";
import { subscribePrivacyChanges } from "@/lib/privacyEvents";
import type { Message, RenderedHtml, PrivacyMode } from "@/lib/api";

export function useMessageLoader(messageId: string | null, privacyMode: PrivacyMode) {
  const client = useQueryClient();
  const query = useMessageQuery(messageId);
  const message = query.data ?? null;
  const flagsMutation = useUpdateFlagsMutation();
  const [rendered, setRendered] = useState<RenderedHtml | null>(null);
  const [renderError, setRenderError] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);
  const generation = useRef(0);
  const opened = useRef({ id: messageId, attempted: false, unreadVersion: manualUnreadVersion(client, messageId ?? "") });
  const renderedKey = useRef<string | null>(null);
  // A refreshed message with the same ID may carry a new body after sync. Keep
  // that authoritative version in the render key so stale HTML is discarded.
  const wanted = messageId
    ? `${messageId}:${message?.updated_at ?? 0}:${JSON.stringify(privacyMode)}:${revision}`
    : null;
  const current = useRef({ message, wanted });
  current.current = { message, wanted };

  function refreshPrivacy() {
    generation.current++;
    renderedKey.current = null;
    setRendered(null);
    setRevision((value) => value + 1);
  }
  useEffect(() => subscribePrivacyChanges((change) => {
    const m = current.current.message;
    if (change.settings || !m || (change.accountId === m.account_id && (!change.email || change.email.toLowerCase() === m.from_address.trim().toLowerCase()))) refreshPrivacy();
  }), []);

  useEffect(() => {
    if (opened.current.id !== messageId) opened.current = { id: messageId, attempted: false, unreadVersion: manualUnreadVersion(client, messageId ?? "") };
    if (!message || message.id !== messageId || opened.current.attempted) return;
    if (opened.current.unreadVersion !== manualUnreadVersion(client, message.id)) {
      opened.current.attempted = true;
      return;
    }
    // A star action cancels the in-flight opening query before patching. Its
    // idle cached read is not fresh; settlement will resume the local refresh.
    if ((message.is_read || message.capabilities?.modify_flags === false)
      && (query.isFetching || query.isError || client.isMutating({ mutationKey: messageFlagsMutationKey }) > 0)) return;
    // Opening is a one-shot action. Subsequent refetches or an explicit
    // mark-unread must not mark the still-open message read again.
    opened.current.attempted = true;
    if (!message.is_read && message.capabilities?.modify_flags !== false) {
      flagsMutation.mutate({ messageId: message.id, isRead: true });
    }
  }, [messageId, message, query.isFetching, query.isError]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    renderedKey.current = null;
    setRendered(null);
    setRenderError(null);
    if (!messageId || !message || message.id !== messageId || !wanted) return;
    const started = ++generation.current;
    let cancelled = false;
    // Body sanitization/rendering is independent of the small local metadata
    // read and its flag update; slow HTML must not delay read acknowledgement.
    getRenderedHtml(messageId, privacyMode).then((html) => {
      if (!cancelled && generation.current === started && current.current.wanted === wanted) {
        renderedKey.current = wanted;
        setRendered({ ...html, html: sanitizeHtml(html.html) });
      }
    }).catch((err) => {
      if (!cancelled && generation.current === started) setRenderError(err instanceof Error ? err.message : String(err));
    });
    return () => { cancelled = true; };
  }, [messageId, message?.id, wanted]); // eslint-disable-line react-hooks/exhaustive-deps

  const visibleRendered = wanted && renderedKey.current === wanted ? rendered : null;
  const error = query.error ? (query.error instanceof Error ? query.error.message : String(query.error)) : renderError;
  const loading = query.isLoading || (!!message && !visibleRendered && !error);
  function setMessage(next: Message | null) {
    if (messageId && (!next || next.id === messageId)) client.setQueryData(messageQueryKey(messageId), next);
  }
  return { message, setMessage, rendered: visibleRendered, loading, error, refreshPrivacy };
}
