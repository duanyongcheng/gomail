import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useIsMutating } from "@tanstack/react-query";
import { messageFlagsMutationKey } from "@/hooks/mutations/messageFlagsMutation";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useTranslation } from "react-i18next";
import { Star } from "lucide-react";
import { useStarredMessagesQuery } from "@/hooks/queries";
import { useMailStore } from "@/stores/mail.store";
import { extractErrorMessage } from "@/lib/extractErrorMessage";
import MessageItem from "@/components/MessageItem";
import MessageDetail from "@/components/MessageDetail";
import { useCloseMessageDetail } from "@/hooks/useCloseMessageDetail";
import { useUIStore } from "@/stores/ui.store";
import { getAppTheme } from "@/lib/appThemes";

export default function StarredView() {
  const { t } = useTranslation();
  const appTheme = useUIStore((s) => s.appTheme);
  const rowHeight = getAppTheme(appTheme).rowHeight;
  const activeAccountId = useMailStore((s) => s.activeAccountId);
  const [stateAccountId, setStateAccountId] = useState(activeAccountId);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const pendingActions = useIsMutating({ mutationKey: messageFlagsMutationKey });
  const accountStateMatches = stateAccountId === activeAccountId;
  const scopedSelectedId = accountStateMatches ? selectedId : null;
  const { data: messages, loading, error, hasNextPage, isFetchingNextPage, fetchNextPage, refetch } =
    useStarredMessagesQuery(activeAccountId);

  useCloseMessageDetail(scopedSelectedId !== null, () => setSelectedId(null));

  useEffect(() => {
    if (stateAccountId === activeAccountId) return;
    setStateAccountId(activeAccountId);
    setSelectedId(null);
  }, [activeAccountId, stateAccountId]);

  const visibleMessages = useMemo(() => {
    const seen = new Set<string>();
    return messages.filter((message) => {
      if (!message.is_starred || seen.has(message.id)) return false;
      seen.add(message.id);
      return true;
    });
  }, [messages]);

  const parentRef = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: visibleMessages.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => rowHeight,
    measureElement: (el) => el.getBoundingClientRect().height,
    overscan: 5,
  });

  useEffect(() => { virtualizer.measure(); }, [appTheme, virtualizer]);

  function handleOpen(messageId: string) {
    setSelectedId(messageId);
  }

  const handleLoadMore = useCallback(() => {
    if (hasNextPage && !isFetchingNextPage && !pendingActions) fetchNextPage();
  }, [hasNextPage, isFetchingNextPage, pendingActions, fetchNextPage]);

  const totalCount = visibleMessages.length;

  if (error && messages.length === 0) {
    return (
      <div className="fade-in mail-state-center">
        <Star size={36} strokeWidth={1.3} />
        <p className="mail-state-error">
          {t("starred.loadError", "Failed to load starred messages")}
        </p>
        <p className="mail-state-detail">{extractErrorMessage(error)}</p>
        <button
          type="button"
          className="mail-state-secondary-btn"
          onClick={() => refetch()}
        >
          {t("common.retry", "Retry")}
        </button>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="mail-state-center">
        <div style={{ display: "inline-flex", alignItems: "center", gap: "8px", fontSize: "13px" }}>
          <Star size={18} className="spinner" />
          <span>{t("common.loading", "Loading...")}</span>
        </div>
      </div>
    );
  }

  if (visibleMessages.length === 0 && !hasNextPage) {
    return (
      <div className="fade-in mail-state-center">
        <Star size={36} strokeWidth={1.3} />
        <p className="mail-state-detail">{t("starred.empty", "No starred messages")}</p>
      </div>
    );
  }

  return (
    <div className={`fade-in secondary-mail-workspace${scopedSelectedId ? " secondary-mail-workspace--detail-open" : ""}`}>
      <div className="secondary-mail-list">
        <div className="secondary-mail-heading">
          <Star size={16} aria-hidden="true" />
          <span>{t("starred.title", "Starred Messages")}</span>
          <span className="secondary-mail-count">
            {totalCount}
          </span>
        </div>

        <div ref={parentRef} className="scroll-region starred-list-scroll" style={{ flex: 1, overflow: "auto" }}>
          <div style={{ height: `${virtualizer.getTotalSize()}px`, width: "100%", position: "relative" }}>
            {virtualizer.getVirtualItems().map((virtualItem) => {
              const msg = visibleMessages[virtualItem.index];
              return (
                <div
                  key={msg.id}
                  ref={virtualizer.measureElement}
                  data-index={virtualItem.index}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    transform: `translateY(${virtualItem.start}px)`,
                  }}
                >
                  <MessageItem
                    rowIndex={virtualItem.index}
                    message={msg}
                    isSelected={msg.id === scopedSelectedId}
                    onClick={() => handleOpen(msg.id)}
                  />
                </div>
              );
            })}
          </div>
        </div>

        {hasNextPage && (
          <div style={{
            borderTop: "1px solid var(--color-border)",
            padding: "10px 16px",
            display: "flex",
            justifyContent: "center",
          }}>
            <button
              type="button"
              className="mail-state-secondary-btn"
              onClick={handleLoadMore}
              disabled={isFetchingNextPage || pendingActions > 0}
              style={{
                cursor: isFetchingNextPage ? "default" : "pointer",
                opacity: isFetchingNextPage ? 0.7 : 1,
              }}
            >
              {t("common.loadMore", "Load More")}
            </button>
          </div>
        )}
      </div>

      {scopedSelectedId && (
        <div className="secondary-mail-detail starred-detail-scroll">
          <MessageDetail
            messageId={scopedSelectedId}
            onBack={() => setSelectedId(null)}
          />
        </div>
      )}
    </div>
  );
}
