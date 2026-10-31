import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, QueryObserver, onlineManager, type InfiniteData, type QueryKey } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { executeBatchMailAction, executeMarkAllRead, executeMembershipAction } from "@/hooks/mutations/mailActionsMutation";
import { executeMessageFlags, messageFlagsMutationKey, refreshMessageFlagViews } from "@/hooks/mutations/messageFlagsMutation";
import { useMessageLoader } from "@/hooks/useMessageLoader";
import { manualUnreadVersion } from "@/hooks/queries/messageFlagsCache";
import { archiveMessage, batchArchive, batchDelete, batchMarkRead, batchStar, deleteMessage, getMessage, getRenderedHtml, markAccountAllRead, markFolderAllRead, moveToFolder, restoreMessage, updateMessageFlags } from "@/lib/api";
import type { Message } from "@/lib/api";
vi.mock("@/lib/api", () => ({ archiveMessage: vi.fn(), batchArchive: vi.fn(), batchDelete: vi.fn(), batchMarkRead: vi.fn(), batchStar: vi.fn(), deleteMessage: vi.fn(), restoreMessage: vi.fn(), moveToFolder: vi.fn(), markAccountAllRead: vi.fn(), markFolderAllRead: vi.fn(), updateMessageFlags: vi.fn(), getMessage: vi.fn(), getRenderedHtml: vi.fn() }));
const base = { id: "a:one", account_id: "a", is_read: true, is_starred: false, capabilities: { modify_flags: true } } as Message;
const other = { ...base, id: "b:two", account_id: "b" };
const keys: QueryKey[] = [["message", base.id], ["messages", "a:inbox", undefined], ["messages", "b:inbox", ["b:inbox", "a:inbox"]], ["starred-messages", "a"], ["threadMessages", "thread:a"]];
let client: QueryClient;
let persisted: Message[];
let stops: Array<() => void>;
let wasOnline: boolean;
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (error: Error) => void; const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
function data(key: QueryKey): unknown {
  if (key[0] === "message") return { ...persisted[0] };
  if (key[0] === "threadMessages") return structuredClone(persisted);
  return { pages: [[{ ...persisted[0] }], [{ ...persisted[1] }]], pageParams: [0, 50] };
}
function message(key: QueryKey, id = base.id) {
  const value = client.getQueryData(key);
  if (key[0] === "message") return value as Message;
  if (key[0] === "threadMessages") return (value as Message[]).find((m) => m.id === id)!;
  return (value as InfiniteData<Message[]>).pages.flat().find((m) => m.id === id)!;
}
function wrapper({ children }: { children: ReactNode }) { return <QueryClientProvider client={client}>{children}</QueryClientProvider>; }
const t = (_key: string, fallback: string) => fallback;
beforeEach(() => {
  vi.resetAllMocks(); wasOnline = onlineManager.isOnline(); onlineManager.setOnline(true);
  client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } } });
  persisted = [{ ...base }, { ...other }]; stops = [];
  keys.forEach((key, i) => client.setQueryData(key, data(key), { updatedAt: i + 1 }));
  vi.mocked(updateMessageFlags).mockImplementation(async (id, read, star) => { const m = persisted.find((m) => m.id === id)!; if (read !== undefined) m.is_read = read; if (star !== undefined) m.is_starred = star; });
  vi.mocked(getMessage).mockImplementation(async (id) => ({ ...persisted.find((m) => m.id === id)! }));
  vi.mocked(getRenderedHtml).mockResolvedValue({ html: "<p>Body</p>", trackers_blocked: [], images_blocked: 0 });
});
afterEach(() => { cleanup(); stops.forEach((stop) => stop()); client.clear(); onlineManager.setOnline(wasOnline); });

describe("receipt-only bulk and membership mail actions", () => {
  it.each(["markRead", "markUnread", "star", "unstar", "archive", "delete"] as const)("%s deduplicates IDs, waits for receipt and never guesses skipped IDs", async (action) => {
    const write = deferred<number>();
    const rpc = action === "archive" ? batchArchive : action === "delete" ? batchDelete : action === "star" || action === "unstar" ? batchStar : batchMarkRead;
    vi.mocked(rpc).mockReturnValue(write.promise);
    const done = executeBatchMailAction(client, action, [base.id, base.id, "missing", other.id]);
    await waitFor(() => expect(rpc).toHaveBeenCalledOnce());
    expect(vi.mocked(rpc).mock.calls[0][0]).toEqual([base.id, "missing", other.id]);
    expect(client.isMutating({ mutationKey: messageFlagsMutationKey })).toBe(1);
    keys.forEach((key) => expect(message(key)).toMatchObject({ is_read: true, is_starred: false }));
    if (action === "markUnread") expect(manualUnreadVersion(client, base.id)).toBeDefined();
    await act(async () => { write.resolve(1); expect(await done).toBe(1); });
    keys.forEach((key) => expect(message(key)).toMatchObject({ is_read: true, is_starred: false }));
  });

  it.each(["archive", "delete", "restore", "move"] as const)("%s is acknowledgement-first and executes durable local IPC offline", async (action) => {
    onlineManager.setOnline(false);
    const write = deferred<any>(); const rpc = action === "archive" ? archiveMessage : action === "delete" ? deleteMessage : action === "restore" ? restoreMessage : moveToFolder;
    vi.mocked(rpc).mockReturnValue(write.promise);
    const done = executeMembershipAction(client, action === "move" ? { action, messageId: base.id, targetFolderId: "a:spam" } : { action, messageId: base.id });
    await waitFor(() => expect(rpc).toHaveBeenCalledOnce());
    expect(client.isMutating({ mutationKey: messageFlagsMutationKey })).toBe(1);
    expect(message(keys[1]).id).toBe(base.id);
    await act(async () => { write.resolve(action === "archive" ? "archived" : undefined); await done; });
    expect(client.isMutating()).toBe(0);
  });

  it("each bulk receipt refreshes counts but sync/queue message refresh waits for all shared writes", async () => {
    const star = deferred<void>(); const read = deferred<number>();
    vi.mocked(updateMessageFlags).mockReturnValue(star.promise); vi.mocked(batchMarkRead).mockReturnValue(read.promise);
    const fetches = keys.map((key) => { const fn = vi.fn(async () => data(key)); const observer = new QueryObserver(client, { queryKey: key, queryFn: fn }); stops.push(observer.subscribe(() => {})); return fn; });
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const first = executeMessageFlags(client, { messageId: base.id, toggleStarred: true }, t);
    const second = executeBatchMailAction(client, "markRead", [base.id, other.id]);
    await waitFor(() => expect(batchMarkRead).toHaveBeenCalledOnce());
    act(() => { refreshMessageFlagViews(client); refreshMessageFlagViews(client); });
    await act(async () => { persisted[0].is_read = true; read.resolve(2); await second; });
    expect(client.isMutating()).toBe(1);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["folder-unread-counts"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["account-unread-counts"] });
    fetches.forEach((fn) => expect(fn).not.toHaveBeenCalled());
    keys.forEach((key) => expect(message(key).is_starred).toBe(true));
    await act(async () => { persisted[0].is_starred = true; star.resolve(); await first; });
    await waitFor(() => expect(client.isFetching()).toBe(0));
    fetches.forEach((fn) => expect(fn).toHaveBeenCalledOnce());
    keys.forEach((key) => expect(message(key).is_starred).toBe(true));
  });

  it("a bulk field receipt retires older rollback ownership without guessing changed rows", async () => {
    persisted[0].is_starred = true;
    keys.forEach((key) => client.setQueryData(key, data(key)));
    const old = deferred<void>(); const hold = deferred<void>();
    vi.mocked(updateMessageFlags).mockImplementationOnce(() => old.promise).mockImplementationOnce(() => hold.promise);
    vi.mocked(batchStar).mockResolvedValue(1);
    const failed = executeMessageFlags(client, { messageId: base.id, isStarred: false }, t).catch(() => {});
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledOnce());
    const holding = executeMessageFlags(client, { messageId: other.id, isRead: false }, t);
    await act(async () => { await executeBatchMailAction(client, "unstar", [base.id, "missing"]); });
    await act(async () => { old.reject(new Error("old write failed")); await failed; });
    keys.forEach((key) => expect(message(key).is_starred).toBe(false));
    await act(async () => { hold.resolve(); await holding; });
  });

  it("batch manual-unread during initial open metadata prevents automatic read after receipt", async () => {
    const metadata = deferred<Message>(); const unread = deferred<number>();
    vi.mocked(getMessage).mockImplementationOnce(() => metadata.promise);
    vi.mocked(batchMarkRead).mockImplementation(async () => { await unread.promise; persisted[0].is_read = false; return 1; });
    const { result } = renderHook(() => useMessageLoader(base.id, "Strict"), { wrapper });
    await waitFor(() => expect(getMessage).toHaveBeenCalledOnce());
    const done = executeBatchMailAction(client, "markUnread", [base.id]);
    await waitFor(() => expect(batchMarkRead).toHaveBeenCalledOnce());
    await act(async () => { metadata.resolve({ ...base, is_read: false }); unread.resolve(1); await done; });
    await waitFor(() => expect(result.current.message?.is_read).toBe(false));
    expect(updateMessageFlags).not.toHaveBeenCalled();
  });

  it("account and folder mark-all preserve scope and cannot overwrite a pending newer star", async () => {
    onlineManager.setOnline(false); const all = deferred<number>(); const hold = deferred<void>();
    vi.mocked(markAccountAllRead).mockReturnValue(all.promise); vi.mocked(markFolderAllRead).mockResolvedValue(0); vi.mocked(updateMessageFlags).mockReturnValue(hold.promise);
    const first = executeMarkAllRead(client, { accountId: "a" });
    await waitFor(() => expect(markAccountAllRead).toHaveBeenCalledWith("a"));
    const star = executeMessageFlags(client, { messageId: base.id, toggleStarred: true }, t);
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledOnce());
    await act(async () => { await executeMarkAllRead(client, { accountId: "b", folderIds: [] }); all.resolve(0); await first; });
    expect(markFolderAllRead).toHaveBeenCalledWith("b", []);
    keys.forEach((key) => expect(message(key).is_starred).toBe(true));
    await act(async () => { hold.resolve(); await star; });
  });
});
