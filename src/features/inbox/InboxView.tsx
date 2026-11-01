import { useMailStore } from "@/stores/mail.store";
import { useComposeStore } from "@/stores/compose.store";
import { useAccountsQuery, useMessagesQuery, useThreadsQuery, useFoldersForAccountsQuery } from "@/hooks/queries";
import { useUIStore } from "@/stores/ui.store";
import { useToastStore } from "@/stores/toast.store";
import MessageList from "@/components/MessageList";
import MessageDetail from "@/components/MessageDetail";
import ThreadView from "./ThreadView";
import ThreadItem from "@/components/ThreadItem";
import SearchBar from "@/components/SearchBar";
import { useRef, useState, useCallback, useEffect, useMemo } from "react";
import ConfirmDialog from "@/components/ConfirmDialog";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { List, MessageSquare, Mail, Trash2, Inbox, CheckSquare } from "lucide-react";
import { MessageListSkeleton } from "@/components/Skeleton";
import { emptyTrash, triggerSync } from "@/lib/api";
import { extractErrorMessage } from "@/lib/extractErrorMessage";
import { folderIdsForSelection, roleForSelection } from "@/lib/folderAggregation";
import { accountBadges, type AccountBadgeInfo } from "@/lib/accountIdentity";
import { useCloseMessageDetail } from "@/hooks/useCloseMessageDetail";
import type { ThreadSummary } from "@/lib/api";
import { isFeatureAvailable, unavailableReason } from "@/lib/availability";
import { refreshMessageFlagViews } from "@/hooks/mutations/messageFlagsMutation";
import { getAppTheme } from "@/lib/appThemes";

const EMPTY_THREADS: ThreadSummary[] = [];

export default function InboxView() {
  const { t } = useTranslation();
  const setActiveView = useUIStore((s) => s.setActiveView);
  const activeFolderId = useMailStore((s) => s.activeFolderId);
  const activeAccountId = useMailStore((s) => s.activeAccountId);
  const selectedMessageId = useMailStore((s) => s.selectedMessageId);
  const setSelectedMessage = useMailStore((s) => s.setSelectedMessage);
  const threadView = useMailStore((s) => s.threadView);
  const toggleThreadView = useMailStore((s) => s.toggleThreadView);
  const selectedThreadId = useMailStore((s) => s.selectedThreadId);
  const setSelectedThreadId = useMailStore((s) => s.setSelectedThreadId);
  const { data: accounts = [] } = useAccountsQuery();
  // Only the combined inbox mixes mailboxes, so only it needs to label rows.
  const showAccountBadges = !activeAccountId && accounts.length > 1;
  const accountBadgesById = useMemo(() => accountBadges(accounts), [accounts]);
  const folderAccountIds = useMemo(
    () => activeAccountId ? [activeAccountId] : accounts.map((account) => account.id),
    [accounts, activeAccountId],
  );
  const { data: folders = [] } = useFoldersForAccountsQuery(folderAccountIds);
  const queryClient = useQueryClient();
  const addToast = useToastStore((s) => s.addToast);
  const [showTrashConfirm, setShowTrashConfirm] = useState(false);
  const [syncingMailbox, setSyncingMailbox] = useState(false);

  // Shown when the selected mailbox has no folders locally, which means its first
  // sync either never ran or did not finish.
  const syncMailbox = useCallback(async () => {
    const targets = activeAccountId ? [activeAccountId] : accounts.map((account) => account.id);
    if (targets.length === 0) return;
    setSyncingMailbox(true);
    try {
      for (const accountId of targets) {
        await triggerSync(accountId, "manual");
      }
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["folders"] }),
        Promise.resolve(refreshMessageFlagViews(queryClient)),
        queryClient.invalidateQueries({ queryKey: ["account-unread-counts"] }),
      ]);
    } catch (err) {
      addToast({ message: extractErrorMessage(err), type: "error" });
    } finally {
      setSyncingMailbox(false);
    }
  }, [activeAccountId, accounts, queryClient, addToast]);

  const activeFolderRole = roleForSelection(activeFolderId, folders);
  const isTrashFolder = activeFolderRole === "trash";
  const selectedFolderIds = folderIdsForSelection(activeFolderId, folders);
  const queryFolderId = selectedFolderIds[0] ?? null;
  const queryFolderIds = selectedFolderIds.length > 1 ? selectedFolderIds : undefined;
  const draftAccounts = activeFolderRole === "drafts" && isFeatureAvailable("cloudDraftCopies")
    ? accounts.filter((account) => account.provider === "gmail" && folders.some((folder) => folder.account_id === account.id && selectedFolderIds.includes(folder.id)))
    : [];

  const {
    data: messages,
    isLoading: loadingMessages,
    hasNextPage,
    isFetchingNextPage,
    fetchNextPage,
  } = useMessagesQuery(
    threadView ? null : queryFolderId,
    threadView ? undefined : queryFolderIds,
  );
  const { data: threads = EMPTY_THREADS, isLoading: loadingThreads, isError: threadsError, refetch: retryThreads, hasNextPage: moreThreads, isFetchingNextPage: loadingMoreThreads, fetchNextPage: loadMoreThreads } = useThreadsQuery(
    threadView ? queryFolderId : null,
    50,
    0,
    threadView ? queryFolderIds : undefined,
  );
  const handleLoadMore = useCallback(() => {
    if (hasNextPage && !isFetchingNextPage) fetchNextPage();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  const detailOpen = threadView ? selectedThreadId !== null : selectedMessageId !== null;
  const folderTitle = activeFolderRole
    ? t(`sidebar.${activeFolderRole}`)
    : folders.find((folder) => folder.id === activeFolderId)?.name ?? t("sidebar.mail");
  const loadedCount = threadView ? threads.length : messages.length;
  const loadedCountLabel = threadView
    ? t("inbox.loadedThreads", "{{count}} conversations loaded", { count: loadedCount })
    : t("inbox.loadedMessages", "{{count}} loaded", { count: loadedCount });
  const moreAvailable = threadView ? moreThreads : hasNextPage;
  const mailboxLabel = activeAccountId
    ? accounts.find((account) => account.id === activeAccountId)?.account_label || accounts.find((account) => account.id === activeAccountId)?.email
    : t("sidebar.allAccounts", "All accounts");

  // Escape closes whatever this view has open — a thread or a single message.
  useCloseMessageDetail(detailOpen, () => {
    if (threadView) setSelectedThreadId(null);
    else setSelectedMessage(null);
  });

  const emptyStateStyle: React.CSSProperties = {
    display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center",
    height: "100%", gap: "16px", color: "var(--color-text-secondary)",
  };
  const primaryButtonStyle: React.CSSProperties = {
    marginTop: "8px", padding: "8px 20px", borderRadius: "6px",
    border: "none", backgroundColor: "var(--color-accent)", color: "#fff",
    fontSize: "13px", fontWeight: 600, cursor: "pointer",
  };

  // Nothing configured at all — welcome / setup prompt.
  if (accounts.length === 0) {
    return (
      <div className="fade-in" style={emptyStateStyle}>
        <Mail size={48} strokeWidth={1.2} />
        <p style={{ fontSize: "16px", fontWeight: 500, color: "var(--color-text-primary)", margin: 0 }}>
          {t("inbox.welcome", "Welcome to Pebble")}
        </p>
        <p style={{ fontSize: "13px", margin: 0 }}>
          {t("inbox.addAccountHint", "Add an email account to get started")}
        </p>
        <button onClick={() => setActiveView("settings")} style={primaryButtonStyle}>
          {t("settings.addAccount", "Add Account")}
        </button>
      </div>
    );
  }

  if (!activeFolderId) {
    // Folders exist, so the sidebar is one render away from auto-selecting one.
    if (folders.length > 0) {
      return <MessageListSkeleton />;
    }
    // No folders at all: this mailbox has never finished a sync. Asking the user
    // to add an account they already configured would send them in circles.
    return (
      <div className="fade-in" style={emptyStateStyle}>
        <Mail size={48} strokeWidth={1.2} />
        <p style={{ fontSize: "16px", fontWeight: 500, color: "var(--color-text-primary)", margin: 0 }}>
          {t("inbox.mailboxNotSynced", "This mailbox has not synced yet")}
        </p>
        <p style={{ fontSize: "13px", margin: 0, textAlign: "center", maxWidth: "340px" }}>
          {t("inbox.mailboxNotSyncedHint", "No folders were found for it locally. Sync now to fetch its mail.")}
        </p>
        <button
          onClick={syncMailbox}
          disabled={syncingMailbox}
          style={{
            ...primaryButtonStyle,
            cursor: syncingMailbox ? "default" : "pointer",
            opacity: syncingMailbox ? 0.6 : 1,
          }}
        >
          {syncingMailbox
            ? t("inbox.syncing", "Syncing...")
            : t("inbox.syncNow", "Sync now")}
        </button>
      </div>
    );
  }

  return (
    <div className={`mail-workspace${detailOpen ? " mail-workspace--detail-open" : ""}`}>
      <section className="mail-list-panel" aria-label={folderTitle}>
        <header className="mail-list-heading">
          <div className="mail-list-kicker">{mailboxLabel}</div>
          <div className="mail-list-title-row">
            <h1 className="mail-list-title">{folderTitle}</h1>
            <span
              className="mail-list-count"
              title={loadedCountLabel}
              aria-label={loadedCountLabel}
            >
              {loadedCount.toLocaleString()}{moreAvailable ? "+" : ""}
            </span>
            <div className="mail-list-view-tools">
              {!threadView && (
                <button
                  type="button"
                  onClick={() => useMailStore.getState().toggleBatchMode()}
                  aria-label={t("batch.toggle", "Batch select")}
                  className="inbox-toolbar-button"
                  title={t("batch.toggle", "Batch select")}
                >
                  <CheckSquare size={15} />
                </button>
              )}
              <button
                type="button"
                onClick={toggleThreadView}
                aria-label={threadView ? t("inbox.messageView") : t("inbox.threadView")}
                className="inbox-toolbar-button"
                title={threadView ? t("inbox.messageView") : t("inbox.threadView")}
              >
                {threadView ? <List size={15} /> : <MessageSquare size={15} />}
              </button>
            </div>
          </div>
        </header>
        <div className="inbox-toolbar-row">
          <div className="inbox-toolbar-search">
            <SearchBar onSearch={() => {}} onClear={() => {}} />
          </div>
          {draftAccounts.map((account) => <button
            key={account.id}
            type="button"
            onClick={() => useComposeStore.getState().openCompose("new", null, { gmailDraftsAccountId: account.id })}
            style={{ padding: "4px 10px", border: "1px solid var(--color-border)", borderRadius: 4, background: "var(--color-bg-primary)", color: "var(--color-text-primary)", fontSize: 12 }}
            title={account.email}
          >
            {t("cloudDrafts.manage", "Manage Gmail drafts")}{draftAccounts.length > 1 ? `: ${account.email}` : ""}
          </button>)}
          {isTrashFolder && messages.length > 0 && (
            <>
              <button
                disabled={!isFeatureAvailable("permanentDelete")}
                onClick={() => { if (isFeatureAvailable("permanentDelete")) setShowTrashConfirm(true); }}
                style={{
                  background: "none", border: "1px solid var(--color-border)", cursor: "pointer",
                  padding: "4px 10px", borderRadius: "4px", color: "var(--color-text-secondary)",
                  display: "flex", alignItems: "center", gap: "4px", fontSize: "12px", marginRight: "4px",
                }}
                title={isFeatureAvailable("permanentDelete") ? t("messageActions.emptyTrash", "Empty Trash") : unavailableReason()}
              >
                <Trash2 size={14} />
                {t("messageActions.emptyTrash", "Empty Trash")}
              </button>
              {isFeatureAvailable("permanentDelete") && showTrashConfirm && (
                <ConfirmDialog
                  title={t("messageActions.emptyTrash", "Empty Trash")}
                  message={t("messageActions.emptyTrashConfirm", "Permanently delete all messages in Trash?")}
                  destructive
                  onCancel={() => setShowTrashConfirm(false)}
                  onConfirm={async () => {
                    if (!isFeatureAvailable("permanentDelete")) return;
                    setShowTrashConfirm(false);
                    try {
                      const targetAccountIds = activeAccountId ? [activeAccountId] : accounts.map((account) => account.id);
                      const counts = await Promise.all(targetAccountIds.map((accountId) => emptyTrash(accountId)));
                      const count = counts.reduce((sum, current) => sum + current, 0);
                      queryClient.invalidateQueries({ queryKey: ["messages"] });
                      addToast({ message: t("messageActions.emptyTrashSuccess", { count }), type: "success" });
                    } catch {
                      addToast({ message: t("messageActions.emptyTrashFailed"), type: "error" });
                    }
                  }}
                />
              )}
            </>
          )}
        </div>
        <div className="mail-list-rows">
          {threadView ? (
            <ThreadList
              threads={threads}
              selectedThreadId={selectedThreadId}
              onSelectThread={setSelectedThreadId}
              loading={loadingThreads}
              error={threadsError}
              onRetry={() => { void retryThreads(); }}
              hasMore={moreThreads}
              loadingMore={loadingMoreThreads}
              onLoadMore={() => { void loadMoreThreads(); }}
              accountBadgesById={showAccountBadges ? accountBadgesById : undefined}
            />
          ) : (
            <MessageList
              messages={messages}
              selectedMessageId={selectedMessageId}
              onSelectMessage={setSelectedMessage}
              loading={loadingMessages}
              onLoadMore={handleLoadMore}
              hasNextPage={hasNextPage}
              isFetchingNextPage={isFetchingNextPage}
            />
          )}
        </div>
      </section>
      <section className={`mail-reader-panel${detailOpen ? "" : " mail-reader-panel--empty"}`} aria-label={t("inbox.emailContent", "Email content")}>
        {detailOpen ? (
          <>
            {threadView && selectedThreadId ? (
              <ThreadView />
            ) : selectedMessageId ? (
              <MessageDetail
                messageId={selectedMessageId}
                onBack={() => setSelectedMessage(null)}
                folderRole={activeFolderRole}
              />
            ) : null}
          </>
        ) : (
          <div className="mail-reader-empty">
            <div className="mail-reader-empty-mark" aria-hidden="true"><Mail size={30} strokeWidth={1.1} /></div>
            <h2>{t("inbox.selectMessage", "Your next good read")}</h2>
            <p>{t("inbox.selectMessageHint", "Select a message or conversation from the list to read it here.")}</p>
          </div>
        )}
      </section>
    </div>
  );
}

// Inline ThreadList component using virtualizer
function ThreadList({ threads, selectedThreadId, onSelectThread, loading, error, onRetry, hasMore, loadingMore, onLoadMore, accountBadgesById }: {
  threads: ThreadSummary[];
  selectedThreadId: string | null;
  onSelectThread: (id: string) => void;
  loading: boolean;
  error?: boolean;
  onRetry: () => void;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore: () => void;
  /** Absent when a single mailbox is selected, where labelling every row is noise. */
  accountBadgesById?: Map<string, AccountBadgeInfo>;
}) {
  const { t } = useTranslation();
  const parentRef = useRef<HTMLDivElement>(null);
  const appTheme = useUIStore((s) => s.appTheme);
  const rowHeight = getAppTheme(appTheme).rowHeight;
  const virtualizer = useVirtualizer({
    count: threads.length,
    getItemKey: (index) => threads[index].thread_id,
    getScrollElement: () => parentRef.current,
    estimateSize: () => rowHeight,
    measureElement: (el) => el.getBoundingClientRect().height,
    overscan: 5,
  });

  useEffect(() => {
    virtualizer.measure();
  }, [appTheme, virtualizer]);

  // Scroll selected thread into view on keyboard navigation
  useEffect(() => {
    if (!selectedThreadId) return;
    const idx = threads.findIndex((t) => t.thread_id === selectedThreadId);
    if (idx >= 0) {
      virtualizer.scrollToIndex(idx, { align: "auto" });
    }
  }, [selectedThreadId, threads, virtualizer]);

  if (loading) {
    return <MessageListSkeleton />;
  }

  if (error) return <div role="alert">{t("thread.loadFailed", "Could not load conversation. Try again.")} <button onClick={onRetry}>{t("common.retry", "Retry")}</button></div>;
  if (threads.length === 0) {
    return (
      <div className="fade-in" style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%", color: "var(--color-text-secondary)", fontSize: "14px", gap: "8px" }}>
        <Inbox size={32} strokeWidth={1.2} />
        {t("common.noThreads")}
      </div>
    );
  }

  return (
    <div ref={parentRef} className="scroll-region thread-list-scroll" style={{ height: "100%", overflow: "auto" }}>
      <div
        role="listbox"
        aria-label={t("inbox.threadList", "Threads")}
        style={{ height: `${virtualizer.getTotalSize()}px`, position: "relative" }}
      >
        {virtualizer.getVirtualItems().map((virtualItem) => {
          const thread = threads[virtualItem.index];
          return (
            <div
              key={virtualItem.key}
              ref={virtualizer.measureElement}
              data-index={virtualItem.index}
              style={{
                position: "absolute", top: 0, left: 0, width: "100%",
                transform: `translateY(${virtualItem.start}px)`,
              }}
            >
              <ThreadItem
                rowIndex={virtualItem.index}
                thread={thread}
                isSelected={thread.thread_id === selectedThreadId}
                onClick={() => onSelectThread(thread.thread_id)}
                accountBadge={accountBadgesById?.get(thread.account_id)}
              />
            </div>
          );
        })}
      </div>
      {hasMore && <button disabled={loadingMore} onClick={onLoadMore} style={{ margin: 12 }}>{t("thread.loadMore", "Load more conversations")}</button>}
    </div>
  );
}
