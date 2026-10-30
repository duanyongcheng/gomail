import { act, cleanup, fireEvent, render, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, QueryObserver, type InfiniteData, type QueryKey } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { queryClient } from "@/lib/query-client";
import { useKeyboard } from "@/hooks/useKeyboard";
import { useMessageLoader } from "@/hooks/useMessageLoader";
import { useUpdateFlagsMutation } from "@/hooks/mutations/useUpdateFlagsMutation";
import { messageFlagsMutationKey } from "@/hooks/mutations/messageFlagsMutation";
import { buildCommands } from "@/features/command-palette/commands";
import StatusBar from "@/components/StatusBar";
import { useMailStore } from "@/stores/mail.store";
import { useShortcutStore } from "@/stores/shortcut.store";
import { useToastStore } from "@/stores/toast.store";
import { useUIStore } from "@/stores/ui.store";
import { getMessage, getRenderedHtml, updateMessageFlags } from "@/lib/api";
import type { Message } from "@/lib/api";
import "@/lib/i18n";

const listeners = vi.hoisted(() => new Map<string, (event: { payload: unknown }) => void>());
vi.mock("@tauri-apps/api/event", () => ({ listen: vi.fn(async (name, handler) => { listeners.set(name, handler); return () => listeners.delete(name); }) }));
vi.mock("@/lib/api", () => ({ getMessage: vi.fn(), getRenderedHtml: vi.fn(), updateMessageFlags: vi.fn(), archiveMessage: vi.fn(), stopSync: vi.fn() }));
// Keep a real isolated client on the existing imperative injection surface.
vi.mock("@/lib/query-client", async () => {
  const { QueryClient } = await import("@tanstack/react-query");
  return { queryClient: new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } } }) };
});
vi.mock("@/hooks/mutations/useSyncMutation", () => ({ useSyncMutation: () => ({ mutateAsync: vi.fn() }) }));
vi.mock("@/hooks/queries", async (original) => ({ ...await original<object>(), usePendingMailOpsSummary: () => ({ data: null }) }));

const base = {
  id: "a:one", account_id: "a", from_address: "sender@example.invalid", is_read: true, is_starred: false,
  updated_at: 1, capabilities: { modify_flags: true }, body_html_raw: "<p>Body</p>",
} as Message;
const second = { ...base, id: "b:two", account_id: "b", is_read: false };
const keys = [["message", base.id], ["messages", "inbox"], ["starred-messages", "a"], ["threadMessages", "thread:a"]] as const;
const viewRoots = ["message", "messages", "starred-messages", "threadMessages", "threads", "search"];
let persisted: Message[];
let stops: Array<() => void>;
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}
function value(key: QueryKey) {
  if (key[0] === "message") return { ...persisted[0] };
  if (key[0] === "threadMessages") return structuredClone(persisted);
  return { pages: [structuredClone(persisted)], pageParams: [0] };
}
function flags(key: QueryKey, id = base.id) {
  const data = queryClient.getQueryData(key);
  if (key[0] === "message") return data as Message;
  if (key[0] === "threadMessages") return (data as Message[]).find((m) => m.id === id)!;
  return (data as InfiniteData<Message[]>).pages.flat().find((m) => m.id === id)!;
}
function command(id: string) {
  return buildCommands((_key, fallback) => fallback).find((cmd) => cmd.id === id)!;
}
function pressStar() { fireEvent.keyDown(document.body, { key: "s" }); }
function expectAll(expected: Partial<Message>) {
  for (const key of keys) expect(flags(key), String(key)).toMatchObject(expected);
}
function observe(key: QueryKey, queryFn = async () => value(key)) {
  const observer = new QueryObserver(queryClient, { queryKey: key, queryFn });
  stops.push(observer.subscribe(() => {}));
}
beforeEach(() => {
  vi.resetAllMocks();
  persisted = [{ ...base }, { ...second }];
  stops = [];
  listeners.clear();
  useToastStore.setState({ toasts: [] });
  useUIStore.setState({ activeView: "inbox", syncStatus: "idle" });
  useMailStore.setState({ selectedMessageId: base.id, activeAccountId: "a", threadView: false });
  useShortcutStore.setState({ bindings: { "toggle-star": "S" }, recording: null });
  for (const key of keys) queryClient.setQueryData(key, value(key));
  vi.mocked(getMessage).mockImplementation(async (id) => ({ ...persisted.find((m) => m.id === id)! }));
  vi.mocked(getRenderedHtml).mockResolvedValue({ html: "<p>Body</p>", trackers_blocked: [], images_blocked: 0 });
  vi.mocked(updateMessageFlags).mockImplementation(async (id, read, star) => {
    const message = persisted.find((m) => m.id === id)!;
    if (read !== undefined) message.is_read = read;
    if (star !== undefined) message.is_starred = star;
  });
});
afterEach(() => { cleanup(); stops.forEach((stop) => stop()); queryClient.clear(); });

describe("keyboard and palette share the flag mutation owner", () => {
  it.each(["keyboard", "palette"])("%s star updates open detail and every cache before acknowledgement, then refreshes them", async (entry) => {
    const write = deferred<void>();
    const persist = vi.mocked(updateMessageFlags).getMockImplementation()!;
    vi.mocked(updateMessageFlags).mockImplementation(async (...args) => { await write.promise; await persist(...args); });
    keys.slice(1).forEach((key) => observe(key));
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => { useKeyboard(); return useMessageLoader(base.id, "Strict"); }, { wrapper });
    await waitFor(() => expect(result.current.rendered).not.toBeNull());
    let done: void | Promise<void>;
    act(() => { if (entry === "keyboard") pressStar(); else done = command("mail:star").execute(); });
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(base.id, undefined, true));
    expect(queryClient.isMutating({ mutationKey: messageFlagsMutationKey })).toBe(1);
    expectAll({ is_starred: true });
    expect(result.current.message?.is_starred).toBe(true);
    expect(persisted[0].is_starred).toBe(false);
    await act(async () => { write.resolve(); await done; });
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expectAll({ is_starred: true });
    expect(persisted[0].is_starred).toBe(true);
    for (const root of viewRoots) expect(invalidate).toHaveBeenCalledWith({ queryKey: [root] });
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: ["account-unread-counts"] });
  });

  it.each(["mail:mark-read", "mail:mark-unread"])("%s optimistically patches all views and refreshes both unread counts", async (id) => {
    const read = id === "mail:mark-read";
    persisted[0].is_read = !read;
    for (const key of keys) queryClient.setQueryData(key, value(key));
    const write = deferred<void>();
    vi.mocked(updateMessageFlags).mockReturnValue(write.promise);
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    let done!: void | Promise<void>;
    act(() => { done = command(id).execute(); });
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(base.id, read, undefined));
    expectAll({ is_read: read });
    await act(async () => { write.resolve(); await done; });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["account-unread-counts"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["folder-unread-counts"] });
    for (const root of viewRoots) expect(invalidate).toHaveBeenCalledWith({ queryKey: [root] });
  });

  it.each(["keyboard", "mail:star", "mail:mark-read", "mail:mark-unread"])("%s failure is handled, rolls back only its fields, and emits one generic toast", async (entry) => {
    const isRead = entry.startsWith("mail:mark-");
    const initialRead = entry !== "mail:mark-read";
    persisted[0].is_read = initialRead;
    for (const key of keys) queryClient.setQueryData(key, value(key));
    const write = deferred<void>();
    vi.mocked(updateMessageFlags).mockImplementationOnce(() => write.promise);
    const { result } = renderHook(() => { useKeyboard(); return useUpdateFlagsMutation(); }, { wrapper });
    let done: void | Promise<void>;
    act(() => { if (entry === "keyboard") pressStar(); else done = command(entry).execute(); });
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledOnce());
    expectAll(isRead ? { is_read: !initialRead } : { is_starred: true });
    await act(async () => {
      await result.current.mutateAsync({ messageId: base.id, ...(isRead ? { isStarred: true } : { isRead: false }) });
      await result.current.mutateAsync({ messageId: second.id, isRead: true });
    });
    await act(async () => { write.reject(new Error("private database path and message")); await done; });
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expectAll(isRead ? { is_read: initialRead, is_starred: true } : { is_read: false, is_starred: false });
    for (const key of keys.slice(1)) expect(flags(key, second.id).is_read).toBe(true);
    expect(useToastStore.getState().toasts.map((toast) => toast.message)).toEqual(["Could not update or open this message."]);
  });

  it("rapid toggles resolve against the latest shared optimistic state and cannot roll back a newer success", async () => {
    const old = deferred<void>();
    const newer = deferred<void>();
    vi.mocked(updateMessageFlags).mockImplementationOnce(() => old.promise).mockImplementationOnce(() => newer.promise);
    renderHook(() => useKeyboard(), { wrapper });
    let palette!: void | Promise<void>;
    act(() => { pressStar(); palette = command("mail:star").execute(); });
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledTimes(2));
    expect(vi.mocked(updateMessageFlags).mock.calls).toEqual([[base.id, undefined, true], [base.id, undefined, false]]);
    expectAll({ is_starred: false });
    expect(queryClient.isMutating({ mutationKey: messageFlagsMutationKey })).toBe(2);
    await act(async () => { newer.resolve(); await palette; });
    await act(async () => old.reject(new Error("late failure")));
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expectAll({ is_starred: false });
  });

  it("uses a newer list observation instead of an inactive stale detail to toggle", async () => {
    for (const key of keys) queryClient.setQueryData(key, value(key), { updatedAt: 1 });
    persisted[0].is_starred = true;
    queryClient.setQueryData(keys[1], value(keys[1]));
    await act(async () => { await command("mail:star").execute(); });
    expect(updateMessageFlags).toHaveBeenCalledWith(base.id, undefined, false);
    expectAll({ is_starred: false });
  });

  it("a read-only patch must not promote an older star observation over a newer list", async () => {
    for (const key of keys) queryClient.setQueryData(key, value(key), { updatedAt: 1 });
    persisted[0].is_starred = true;
    queryClient.setQueryData(keys[1], value(keys[1]), { updatedAt: 2 });
    const read = deferred<void>();
    vi.mocked(updateMessageFlags).mockImplementationOnce(() => read.promise);
    let unread!: void | Promise<void>;
    act(() => { unread = command("mail:mark-unread").execute(); });
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledOnce());
    await act(async () => { await command("mail:star").execute(); });
    expect(updateMessageFlags).toHaveBeenLastCalledWith(base.id, undefined, false);
    await act(async () => { read.resolve(); await unread; });
  });

  it("a failed star patch must not promote older metadata for a different message", async () => {
    const third = { ...base, id: "a:three", is_starred: false };
    const threadKey = keys[3];
    const listKey = keys[1];
    queryClient.setQueryData(threadKey, [{ ...base }, third], { updatedAt: 1 });
    queryClient.setQueryData(listKey, { pages: [[{ ...base }, { ...third, is_starred: true }]], pageParams: [0] }, { updatedAt: 2 });
    const failed = deferred<void>();
    const holding = deferred<void>();
    vi.mocked(updateMessageFlags).mockImplementationOnce(() => failed.promise).mockImplementationOnce(() => holding.promise).mockResolvedValue();
    let first!: void | Promise<void>;
    let other!: void | Promise<void>;
    act(() => { first = command("mail:star").execute(); });
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledOnce());
    // Keep final invalidation deferred so this checks rollback's timestamps,
    // not a refetch that happens to repair the metadata later.
    useMailStore.setState({ selectedMessageId: second.id });
    act(() => { other = command("mail:mark-unread").execute(); });
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledTimes(2));
    await act(async () => { failed.reject(new Error("synthetic failure")); await first; });
    useMailStore.setState({ selectedMessageId: third.id });
    await act(async () => { await command("mail:star").execute(); });
    expect(updateMessageFlags).toHaveBeenLastCalledWith(third.id, undefined, false);
    await act(async () => { holding.resolve(); await other; });
  });

  it("simultaneous hook and palette settlements still refresh filtered starred membership", async () => {
    const write = deferred<void>();
    persisted[0].is_starred = true;
    for (const key of keys) queryClient.setQueryData(key, value(key));
    const persist = vi.mocked(updateMessageFlags).getMockImplementation()!;
    vi.mocked(updateMessageFlags).mockImplementation(async (...args) => { await write.promise; await persist(...args); });
    const starred = vi.fn(async () => ({ pages: [persisted.filter((m) => m.is_starred)], pageParams: [0] }));
    observe(keys[2], starred);
    const { result } = renderHook(() => useUpdateFlagsMutation(), { wrapper });
    let palette!: void | Promise<void>;
    act(() => {
      palette = command("mail:star").execute();
      result.current.mutate({ messageId: second.id, isRead: true });
    });
    await waitFor(() => expect(queryClient.isMutating({ mutationKey: messageFlagsMutationKey })).toBe(2));
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledTimes(2));
    await act(async () => { write.resolve(); await palette; });
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    await waitFor(() => expect(starred).toHaveBeenCalledOnce());
    expect(queryClient.getQueryData<InfiniteData<Message[]>>(keys[2])!.pages[0]).toEqual([]);
  });

  it("keyboard cancels a pre-existing list/detail response without losing optimistic state", async () => {
    const stale = deferred<Message>();
    const oldList = deferred<InfiniteData<Message[]>>();
    const write = deferred<void>();
    vi.mocked(updateMessageFlags).mockReturnValue(write.promise);
    const detail = queryClient.fetchQuery({ queryKey: keys[0], queryFn: () => stale.promise, staleTime: 0 }).catch(() => {});
    const list = queryClient.fetchQuery({ queryKey: keys[1], queryFn: () => oldList.promise, staleTime: 0 }).catch(() => {});
    renderHook(() => useKeyboard(), { wrapper });
    act(pressStar);
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledOnce());
    await act(async () => { stale.resolve({ ...base }); oldList.resolve({ pages: [[{ ...base }]], pageParams: [0] }); await Promise.all([detail, list]); });
    expectAll({ is_starred: true });
    await act(async () => write.resolve());
  });

  it("hook mutation still uses its injected provider client, not the keyboard singleton", async () => {
    const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    client.setQueryData(keys[0], { ...base });
    const { result } = renderHook(() => useUpdateFlagsMutation(), { wrapper: ({ children }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> });
    await act(async () => result.current.mutateAsync({ messageId: base.id, isStarred: true }));
    expect(client.getQueryData<Message>(keys[0])?.is_starred).toBe(true);
    expect(flags(keys[0]).is_starred).toBe(false);
    client.clear();
  });
});

it("StatusBar defers sync/pending/new/label refreshes over concurrent flags, then refreshes every view once safe", async () => {
  const star = deferred<void>();
  const read = deferred<void>();
  const persist = vi.mocked(updateMessageFlags).getMockImplementation()!;
  vi.mocked(updateMessageFlags).mockImplementation(async (...args) => {
    await (args[2] === undefined ? read.promise : star.promise);
    await persist(...args);
  });
  const reads = new Map<string, ReturnType<typeof vi.fn>>();
  for (const key of keys) {
    const fetch = vi.fn(async () => value(key));
    reads.set(String(key[0]), fetch);
    observe(key, fetch);
  }
  const invalidate = vi.spyOn(queryClient, "invalidateQueries");
  render(<StatusBar />, { wrapper });
  renderHook(() => useKeyboard(), { wrapper });
  let palette!: void | Promise<void>;
  act(() => { pressStar(); palette = command("mail:mark-unread").execute(); });
  await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledTimes(2));
  expectAll({ is_read: false, is_starred: true });
  act(() => {
    for (const event of ["mail:pending-ops-changed", "mail:sync-complete", "mail:new", "mail:folders-changed"]) {
      listeners.get(event)!({ payload: { account_id: "a" } });
    }
    listeners.get("mail:sync-progress")!({ payload: { account_id: "a", status: "completed" } });
  });
  for (const fetch of reads.values()) expect(fetch).not.toHaveBeenCalled();
  expectAll({ is_read: false, is_starred: true });
  // Read receipt must refresh counts even with the star still unresolved.
  await act(async () => { read.resolve(); await palette; });
  expect(queryClient.isMutating()).toBe(1);
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ["account-unread-counts"] });
  for (const fetch of reads.values()) expect(fetch).not.toHaveBeenCalled();
  await act(async () => star.resolve());
  await waitFor(() => expect(queryClient.isMutating()).toBe(0));
  await waitFor(() => expect(queryClient.isFetching()).toBe(0));
  for (const fetch of reads.values()) expect(fetch).toHaveBeenCalledOnce();
  for (const root of viewRoots) expect(invalidate).toHaveBeenCalledWith({ queryKey: [root] });
  expectAll({ is_read: false, is_starred: true });
});
