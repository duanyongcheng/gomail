import { useEffect, useRef, useState } from "react";
import { getMessageWithHtml, getRenderedHtml } from "@/lib/api";
import { useUpdateFlagsMutation } from "@/hooks/mutations/useUpdateFlagsMutation";
import { sanitizeHtml } from "@/lib/sanitizeHtml";
import { subscribePrivacyChanges } from "@/lib/privacyEvents";
import type { Message, RenderedHtml, PrivacyMode } from "@/lib/api";

export function useMessageLoader(messageId: string | null, privacyMode: PrivacyMode) {
  const flagsMutation = useUpdateFlagsMutation();
  const [message, setMessage] = useState<Message | null>(null);
  const [rendered, setRendered] = useState<RenderedHtml | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const current = useRef({ id: messageId, mode: privacyMode, revision });
  current.current = { id: messageId, mode: privacyMode, revision };
  const currentMessage = useRef(message);
  currentMessage.current = message;
  const generation = useRef(0);
  const renderedKey = useRef<string | null>(null);
  const key = (id: string, mode: PrivacyMode, rev: number) => `${id}:${JSON.stringify(mode)}:${rev}`;

  function refreshPrivacy() {
    generation.current++;
    renderedKey.current = null;
    setRendered(null);
    setRevision((value) => value + 1);
  }
  useEffect(() => subscribePrivacyChanges((change) => {
    const m = currentMessage.current;
    if (change.settings || !m || (change.accountId === m.account_id && (!change.email || change.email.toLowerCase() === m.from_address.trim().toLowerCase()))) refreshPrivacy();
  }), []);

  useEffect(() => {
    let cancelled = false;
    generation.current++;
    setMessage(null); setRendered(null); setError(null); renderedKey.current = null;
    if (!messageId) { setLoading(false); return; }
    setLoading(true);
    const initial = current.current;
    const started = generation.current;
    getMessageWithHtml(messageId, initial.mode).then((result) => {
      if (cancelled || !result) return;
      const [msg, html] = result;
      setMessage(msg);
      // Metadata can be retained, but old HTML must not appear even briefly
      // after a mode change or trust revocation during the initial request.
      if (started === generation.current && current.current.id === messageId && JSON.stringify(current.current.mode) === JSON.stringify(initial.mode)) {
        renderedKey.current = key(messageId, initial.mode, initial.revision);
        setRendered({ ...html, html: sanitizeHtml(html.html) });
      }
      if (!msg.is_read && msg.capabilities?.modify_flags !== false) flagsMutation.mutate({ messageId, isRead: true });
    }).catch((err) => { if (!cancelled) setError(err instanceof Error ? err.message : String(err)); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [messageId]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!message || !messageId || message.id !== messageId) return;
    const wanted = key(messageId, privacyMode, revision);
    if (renderedKey.current === wanted) return;
    const started = ++generation.current;
    let cancelled = false;
    setRendered(null); setError(null);
    getRenderedHtml(messageId, privacyMode).then((html) => {
      if (!cancelled && generation.current === started && current.current.id === messageId && key(messageId, current.current.mode, current.current.revision) === wanted) {
        renderedKey.current = wanted;
        setRendered({ ...html, html: sanitizeHtml(html.html) });
      }
    }).catch((err) => { if (!cancelled && generation.current === started) setError(err instanceof Error ? err.message : String(err)); });
    return () => { cancelled = true; };
  }, [privacyMode, messageId, message, revision]); // eslint-disable-line react-hooks/exhaustive-deps

  const visibleRendered = messageId && renderedKey.current === key(messageId, privacyMode, revision) ? rendered : null;
  return { message, setMessage, rendered: visibleRendered, loading, error, refreshPrivacy };
}
