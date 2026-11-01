import { useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { ArrowLeft, MessageSquare } from "lucide-react";
import { useMailStore } from "@/stores/mail.store";
import { useAccountsQuery, useFoldersForAccountsQuery, useThreadMessagesQuery, useThreadsQuery } from "@/hooks/queries";
import { folderIdsForSelection } from "@/lib/folderAggregation";
import ThreadMessageBubble from "@/components/ThreadMessageBubble";
import Spinner from "@/components/Spinner";
import { useUIStore } from "@/stores/ui.store";
import { useResolvedAppTheme } from "@/hooks/useResolvedAppTheme";
import { getMessageTheme, messageThemeVariables, resolveMessageTheme } from "@/lib/messageThemes";

export default function ThreadView() {
  const { t } = useTranslation();
  const messageThemeId = useUIStore((s) => s.messageTheme);
  const colorMode = useResolvedAppTheme();
  const theme = resolveMessageTheme(getMessageTheme(messageThemeId), colorMode === "dark");
  const selectedThreadId = useMailStore((s) => s.selectedThreadId);
  const setSelectedThreadId = useMailStore((s) => s.setSelectedThreadId);
  const activeFolderId = useMailStore((s) => s.activeFolderId);
  const activeAccountId = useMailStore((s) => s.activeAccountId);
  const { data: accounts = [] } = useAccountsQuery();
  const folderAccountIds = useMemo(
    () => activeAccountId ? [activeAccountId] : accounts.map((account) => account.id),
    [accounts, activeAccountId],
  );
  const { data: folders = [] } = useFoldersForAccountsQuery(folderAccountIds);
  const selectedFolderIds = folderIdsForSelection(activeFolderId, folders);
  const queryFolderId = selectedFolderIds[0] ?? null;
  const queryFolderIds = selectedFolderIds.length > 1 ? selectedFolderIds : undefined;
  const { data: threadMessages = [], isLoading, isError, refetch } = useThreadMessagesQuery(selectedThreadId);
  const { data: threads = [] } = useThreadsQuery(queryFolderId, 50, 0, queryFolderIds);
  const scrollRef = useRef<HTMLDivElement>(null);

  const thread = threads.find((t) => t.thread_id === selectedThreadId);

  const lastMessageId = threadMessages[threadMessages.length - 1]?.id;
  useEffect(() => {
    if (scrollRef.current && lastMessageId) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [selectedThreadId, lastMessageId]);

  if (isLoading) return <Spinner />;
  if (isError) return <div role="alert">
    <p>{t("thread.loadFailed", "Could not load conversation. Try again.")}</p>
    <p>{t("thread.largeHint", "Conversations over 500 cached messages must be opened in message view.")}</p>
    <button onClick={() => void refetch()}>{t("common.retry", "Retry")}</button>
    <button onClick={() => setSelectedThreadId(null)}>{t("thread.back", "Back")}</button>
  </div>;

  return (
    <div className="thread-reader" data-message-theme={theme.id} style={messageThemeVariables(theme)}>
      {/* Header */}
      <div className="thread-reader-header">
        <div style={{ display: "flex", alignItems: "center", gap: "10px" }}>
          <button
            onClick={() => setSelectedThreadId(null)}
            aria-label={t("thread.back", "Back")}
            style={{
              background: "none", border: "none", cursor: "pointer", padding: "4px",
              borderRadius: "4px", color: "var(--color-text-secondary)", display: "flex", alignItems: "center",
            }}
          >
            <ArrowLeft size={18} />
          </button>
          <h2 className="thread-reader-title">
            {thread?.subject || threadMessages[threadMessages.length - 1]?.subject || t("thread.title")}
          </h2>
          <div style={{ display: "flex", alignItems: "center", gap: "4px", color: "var(--color-text-secondary)", fontSize: "12px" }}>
            <MessageSquare size={14} />
            <span>{threadMessages.length}</span>
          </div>
        </div>
      </div>

      {/* Messages */}
      <div ref={scrollRef} className="scroll-region thread-message-scroll">
        {threadMessages.length === 0 && <p>{t("common.noThreads")}</p>}
        {threadMessages.map((msg, i) => (
          <ThreadMessageBubble
            key={msg.id}
            message={msg}
            defaultExpanded={i === threadMessages.length - 1}
            theme={theme}
          />
        ))}
      </div>
    </div>
  );
}
