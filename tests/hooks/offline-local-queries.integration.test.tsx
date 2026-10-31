import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, onlineManager } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useAccountsQuery } from "@/hooks/queries/useAccountsQuery";
import { useFoldersQuery, useFoldersForAccountsQuery } from "@/hooks/queries/useFoldersQuery";
import { useFolderUnreadCounts, useFolderUnreadCountsForAccounts } from "@/hooks/queries/useFolderUnreadCounts";
import { useAccountUnreadCounts } from "@/hooks/queries/useAccountUnreadCounts";
import { useMessagesQuery } from "@/hooks/queries/useMessagesQuery";
import { useMessageQuery } from "@/hooks/queries/useMessageQuery";
import { useThreadsQuery } from "@/hooks/queries/useThreadsQuery";
import { useThreadMessagesQuery } from "@/hooks/queries/useThreadMessagesQuery";
import { useStarredMessagesQuery } from "@/hooks/queries/useStarredMessagesQuery";
import { useSearchQuery } from "@/hooks/queries/useSearchQuery";
import { useAttachmentsQuery } from "@/hooks/queries/useAttachmentsQuery";
import { usePendingMailOpsQuery } from "@/hooks/queries/usePendingMailOpsQuery";
import { usePendingMailOpsSummary } from "@/hooks/queries/usePendingMailOpsSummary";
import { useUIStore } from "@/stores/ui.store";
import type { Account, Attachment, Folder, Message, PendingMailOp, PendingMailOpsSummary, SearchHit, ThreadSummary } from "@/lib/api";

// Keep the actual hooks, compatibility API, command table and generated bindings.
// Only the native transport is replaced; unknown/remote calls fail the test.
const { native } = vi.hoisted(() => ({ native: vi.fn<(method: string, args: unknown) => Promise<unknown>>() }));
vi.mock("mygo-runtime", () => ({ call: native, event: vi.fn(), on: () => () => {} }));

const account = (id: string): Account => ({ id, email: `${id}@example.invalid`, display_name: id, provider: "gmail", created_at: 1, updated_at: 1 });
const folder = (id: string): Folder => ({ id: `${id}:inbox`, account_id: id, remote_id: "INBOX", name: id, folder_type: "folder", role: "inbox", parent_id: null, color: null, is_system: true, sort_order: 0 });
const message = (id: string, subject: string): Message => ({
  id, subject, account_id: id.split(":")[0], remote_id: id, message_id_header: null, in_reply_to: null,
  references_header: null, thread_id: null, snippet: subject, from_address: "sender@example.invalid", from_name: "Sender",
  to_list: [], cc_list: [], bcc_list: [], has_attachments: true, is_read: false, is_starred: true, is_draft: false,
  date: 1, remote_version: null, is_deleted: false, deleted_at: null, created_at: 1, updated_at: 1,
  body_text: subject, body_html_raw: "", capabilities: { modify_flags: true },
});
const thread = (id: string, subject: string): ThreadSummary => ({ thread_id: id, account_id: "a", subject, snippet: subject, last_date: 1, message_count: 1, unread_count: 1, is_starred: false, participants: [], has_attachments: true });
let client: QueryClient;
let revision: number;
let showCounts: boolean;
const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;

beforeEach(() => {
  native.mockReset(); revision = 1;
  showCounts = useUIStore.getState().showFolderUnreadCount;
  useUIStore.setState({ showFolderUnreadCount: true });
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  native.mockImplementation(async (method, args) => {
    const request = args as { accountId?: string; folderId?: string; messageId?: string; threadId?: string; query?: string; byFolder?: boolean; offset?: number; limit?: number };
    const subject = `local revision ${revision}`;
    switch (method) {
      case "AccountService.List": return [account("a"), account("b")];
      case "FolderService.List": return [folder(request.accountId!)];
      case "MessageService.List": return [message(`${request.folderId || request.accountId}:message`, subject)];
      case "MessageService.Get": return message(request.messageId!, subject);
      case "ThreadService.List": return [thread(`${request.folderId}:thread`, subject)];
      case "ThreadService.Messages": return [message(`${request.threadId}:message`, subject)];
      case "MessageService.UnreadCounts": return { [request.byFolder ? `${request.accountId}:inbox` : "a"]: revision };
      case "SearchService.Search": return [{ message_id: "a:message", score: 1, snippet: subject }] satisfies SearchHit[];
      case "MessageService.Attachments": return [{ id: "a:attachment", message_id: request.messageId!, filename: subject, mime_type: "text/plain", size: 1, local_path: null, content_id: null, is_inline: false }] satisfies Attachment[];
      case "PendingOpsService.List": return [{ id: "op", account_id: request.accountId!, message_id: "a:message", op_type: "update_flags", status: "pending", attempts: 0, last_error: null, created_at: 1, updated_at: revision, next_retry_at: null }] satisfies PendingMailOp[];
      case "PendingOpsService.Summary": return { pending_count: revision, in_progress_count: 0, failed_count: 0, total_active_count: revision, last_error: null, updated_at: revision } satisfies PendingMailOpsSummary;
      default: throw new Error(`Unexpected synthetic transport: ${method}`);
    }
  });
  onlineManager.setOnline(false);
});
afterEach(() => {
  cleanup(); client.clear(); onlineManager.setOnline(true);
  useUIStore.setState({ showFolderUnreadCount: showCounts });
});

const cases: { name: string; hook: () => unknown; method: string; keys: readonly unknown[][]; args: unknown }[] = [
  { name: "accounts", hook: useAccountsQuery, method: "AccountService.List", keys: [["accounts"]], args: {} },
  { name: "folders", hook: () => useFoldersQuery("a"), method: "FolderService.List", keys: [["folders", "a"]], args: { accountId: "a" } },
  { name: "combined-account folders", hook: () => useFoldersForAccountsQuery(["a", "b"]), method: "FolderService.List", keys: [["folders", "a"], ["folders", "b"]], args: { accountId: "b" } },
  { name: "folder counts", hook: () => useFolderUnreadCounts("a"), method: "MessageService.UnreadCounts", keys: [["folder-unread-counts", "a"]], args: { accountId: "a", byFolder: true } },
  { name: "combined-account folder counts", hook: () => useFolderUnreadCountsForAccounts(["a", "b"]), method: "MessageService.UnreadCounts", keys: [["folder-unread-counts", "a"], ["folder-unread-counts", "b"]], args: { accountId: "b", byFolder: true } },
  { name: "account counts", hook: useAccountUnreadCounts, method: "MessageService.UnreadCounts", keys: [["account-unread-counts"]], args: { accountId: "", byFolder: false } },
  { name: "message list", hook: () => useMessagesQuery("a:inbox"), method: "MessageService.List", keys: [["messages", "a:inbox"]], args: { folderId: "a:inbox", accountId: "", starred: false, limit: 50, offset: 0 } },
  { name: "uncached message detail", hook: () => useMessageQuery("a:message"), method: "MessageService.Get", keys: [["message", "a:message"]], args: { messageId: "a:message" } },
  { name: "conversation list", hook: () => useThreadsQuery("a:inbox"), method: "ThreadService.List", keys: [["threads", "a:inbox"]], args: { folderId: "a:inbox", folderIds: [], limit: 50, offset: 0 } },
  { name: "conversation detail", hook: () => useThreadMessagesQuery("a:thread"), method: "ThreadService.Messages", keys: [["threadMessages", "a:thread"]], args: { threadId: "a:thread" } },
  { name: "starred list", hook: () => useStarredMessagesQuery("a"), method: "MessageService.List", keys: [["starred-messages", "a"]], args: { folderId: "", accountId: "a", starred: true, limit: 50, offset: 0 } },
  { name: "basic search", hook: () => useSearchQuery("fixture"), method: "SearchService.Search", keys: [["search", "fixture"]], args: { query: "fixture", accountId: "", limit: 50 } },
  { name: "attachment metadata", hook: () => useAttachmentsQuery("a:message"), method: "MessageService.Attachments", keys: [["attachments", "a:message"]], args: { messageId: "a:message" } },
  { name: "pending operation list", hook: () => usePendingMailOpsQuery("a"), method: "PendingOpsService.List", keys: [["pendingMailOpsList", "a"]], args: { accountId: "a", limit: 100 } },
  { name: "pending operation summary", hook: () => usePendingMailOpsSummary(null), method: "PendingOpsService.Summary", keys: [["pendingMailOps", null]], args: { accountId: "", limit: 100 } },
];

it.each(cases)("reads and invalidates $name locally without reconnecting", async ({ hook, method, keys, args }) => {
  renderHook(hook, { wrapper });
  await waitFor(() => expect(native).toHaveBeenCalledWith(method, args));
  await waitFor(() => expect(client.getQueryCache().getAll().every(query => query.state.status === "success")).toBe(true));
  const calls = native.mock.calls.length;
  const previous = client.getQueryCache().getAll().map(query => query.state.data);
  revision = 2;
  await act(async () => { await Promise.all(keys.map(queryKey => client.invalidateQueries({ queryKey }))); });
  expect(native.mock.calls.length).toBeGreaterThan(calls);
  expect(client.getQueryCache().getAll().every(query => query.state.fetchStatus === "idle")).toBe(true);
  if (method !== "AccountService.List" && method !== "FolderService.List") {
    expect(client.getQueryCache().getAll().map(query => query.state.data)).not.toEqual(previous);
  }
  // Explicit active-query refresh must not wait for browser connectivity either.
  const afterInvalidation = native.mock.calls.length;
  await act(async () => { await Promise.all(keys.map(queryKey => client.refetchQueries({ queryKey }))); });
  expect(native.mock.calls.length).toBeGreaterThan(afterInvalidation);
  expect(onlineManager.isOnline()).toBe(false);
});

it("navigates to uncached account, folder, message and conversation queries offline without leaking old results", async () => {
  const { result, rerender } = renderHook(({ accountId }) => ({
    folders: useFoldersQuery(accountId), messages: useMessagesQuery(`${accountId}:inbox`),
    detail: useMessageQuery(`${accountId}:message`), threads: useThreadsQuery(`${accountId}:inbox`),
    threadDetail: useThreadMessagesQuery(`${accountId}:thread`), starred: useStarredMessagesQuery(accountId),
  }), { wrapper, initialProps: { accountId: "a" } });
  await waitFor(() => expect(result.current.messages.data[0]?.account_id).toBe("a"));
  rerender({ accountId: "b" });
  await waitFor(() => {
    expect(result.current.folders.data?.[0].account_id).toBe("b");
    expect(result.current.messages.data[0]?.account_id).toBe("b");
    expect(result.current.detail.data?.id).toBe("b:message");
    expect(result.current.threads.data[0]?.thread_id).toBe("b:inbox:thread");
    expect(result.current.threadDetail.data?.[0].id).toBe("b:thread:message");
    expect(result.current.starred.data[0]?.account_id).toBe("b");
  });
  expect(onlineManager.isOnline()).toBe(false);
});

it.each(["messages", "starred", "threads"] as const)("loads and refreshes multiple %s pages offline", async kind => {
  native.mockImplementation(async (method, args) => {
    const request = args as { offset: number; limit: number };
    expect(method).toBe(kind === "threads" ? "ThreadService.List" : "MessageService.List");
    return request.offset ? [] : Array.from({ length: request.limit }, (_, index) => kind === "threads"
      ? thread(`a:thread:${index}`, `revision ${revision}`) : message(`a:message:${index}`, `revision ${revision}`));
  });
  const { result } = renderHook(() => {
    const messages = useMessagesQuery(kind === "messages" ? "a:inbox" : null);
    const starred = useStarredMessagesQuery(kind === "starred" ? "a" : null);
    const threads = useThreadsQuery(kind === "threads" ? "a:inbox" : null);
    return kind === "messages" ? messages : kind === "starred" ? starred : threads;
  }, { wrapper });
  await waitFor(() => expect(result.current.data).toHaveLength(50));
  await act(async () => { await result.current.fetchNextPage(); });
  await waitFor(() => expect(result.current.hasNextPage).toBe(false));
  expect(native.mock.calls[1][1]).toMatchObject({ offset: 50, limit: 50 });
  revision = 2;
  await act(async () => { await client.invalidateQueries({ queryKey: [kind === "starred" ? "starred-messages" : kind] }); });
  await waitFor(() => expect(result.current.data[0].subject).toBe("revision 2"));
  expect(native.mock.calls.slice(-2).map(([, args]) => (args as { offset: number }).offset)).toEqual([0, 50]);
});

it("refreshes previously cached lists and unread counts after connectivity is lost", async () => {
  onlineManager.setOnline(true);
  const { result } = renderHook(() => ({ messages: useMessagesQuery("a:inbox"), counts: useAccountUnreadCounts() }), { wrapper });
  await waitFor(() => {
    expect(result.current.messages.data[0]?.subject).toBe("local revision 1");
    expect(result.current.counts.a).toBe(1);
  });
  onlineManager.setOnline(false); revision = 2;
  await act(async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: ["messages"] }),
      client.invalidateQueries({ queryKey: ["account-unread-counts"] }),
    ]);
  });
  await waitFor(() => {
    expect(result.current.messages.data[0]?.subject).toBe("local revision 2");
    expect(result.current.counts.a).toBe(2);
  });
  expect(onlineManager.isOnline()).toBe(false);
});

it("retains null/empty and hidden-count query gates while offline", async () => {
  useUIStore.setState({ showFolderUnreadCount: false });
  renderHook(() => {
    useFoldersQuery(null); useFoldersForAccountsQuery([]); useFolderUnreadCounts("a"); useFolderUnreadCountsForAccounts(["a", "b"]);
    useMessagesQuery(null); useThreadsQuery(null); useMessageQuery(null); useThreadMessagesQuery(null);
    useStarredMessagesQuery(null); useSearchQuery(""); useAttachmentsQuery(null);
  }, { wrapper });
  await act(async () => { await Promise.resolve(); });
  expect(native).not.toHaveBeenCalled();
});
