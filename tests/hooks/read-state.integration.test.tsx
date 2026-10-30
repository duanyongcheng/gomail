import { QueryClient, QueryClientProvider, type InfiniteData } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { StrictMode, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getMessage, getRenderedHtml, getMessageWithHtml, updateMessageFlags } from "@/lib/api";
import type { Message, RenderedHtml } from "@/lib/api";
import { useMessageLoader } from "@/hooks/useMessageLoader";
import { useUpdateFlagsMutation } from "@/hooks/mutations/useUpdateFlagsMutation";
import "@/lib/i18n";
import { useToastStore } from "@/stores/toast.store";

vi.mock("@/lib/api", () => ({
  getMessage: vi.fn(),
  getRenderedHtml: vi.fn(),
  getMessageWithHtml: vi.fn(),
  updateMessageFlags: vi.fn(),
}));
vi.mock("@tauri-apps/api/event", () => ({ listen: vi.fn(async () => () => {}) }));

const message: Message = {
  id: "a:one", account_id: "a", remote_id: "one", message_id_header: null,
  in_reply_to: null, references_header: null, thread_id: "thread:a",
  subject: "Unread", snippet: "Body", from_address: "sender@example.invalid",
  from_name: "Sender", to_list: [], cc_list: [], bcc_list: [],
  has_attachments: false, is_read: false, is_starred: true, is_draft: false,
  date: 1, remote_version: null, is_deleted: false, deleted_at: null,
  created_at: 1, updated_at: 1, body_text: "Body", body_html_raw: "<p>Body</p>",
  capabilities: { modify_flags: true },
};
const rendered: RenderedHtml = { html: "<p>Body</p>", trackers_blocked: [], images_blocked: 0 };
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
let client: QueryClient;
let persisted: Message;
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
beforeEach(() => {
  vi.resetAllMocks();
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  persisted = { ...message };
  useToastStore.setState({ toasts: [] });
  vi.mocked(getMessage).mockImplementation(async () => ({ ...persisted }));
  vi.mocked(getRenderedHtml).mockResolvedValue(rendered);
  vi.mocked(getMessageWithHtml).mockResolvedValue([{ ...message }, rendered]);
  vi.mocked(updateMessageFlags).mockImplementation(async (_id, read, starred) => {
    if (read !== undefined) persisted.is_read = read;
    if (starred !== undefined) persisted.is_starred = starred;
  });
});
afterEach(() => { cleanup(); client.clear(); });

describe("opening unread mail", () => {
  it("queues read immediately from local metadata, without waiting for HTML", async () => {
    const html = deferred<RenderedHtml>();
    vi.mocked(getRenderedHtml).mockReturnValue(html.promise);
    vi.mocked(getMessageWithHtml).mockReturnValue(html.promise.then((value) => [{ ...message }, value]));
    client.setQueryData(["messages", "inbox"], { pages: [[{ ...message }]], pageParams: [0] });
    const { result } = renderHook(() => useMessageLoader(message.id, "Strict"), { wrapper });
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(message.id, true, undefined));
    expect(result.current.message?.is_read).toBe(true);
    expect(client.getQueryData<InfiniteData<Message[]>>(["messages", "inbox"])!.pages[0][0].is_read).toBe(true);
    expect(result.current.rendered).toBeNull();
    await act(async () => html.resolve(rendered));
    await waitFor(() => expect(result.current.rendered?.html).toContain("Body"));
    expect(result.current.message?.is_read).toBe(true);
  });

  it("patches starred and conversation metadata before the local acknowledgement", async () => {
    const write = deferred<void>();
    vi.mocked(updateMessageFlags).mockReturnValue(write.promise);
    client.setQueryData(["starred-messages", "a"], { pages: [[{ ...message }]], pageParams: [0] });
    client.setQueryData(["threadMessages", "thread:a"], [{ ...message }]);
    const { result } = renderHook(() => useUpdateFlagsMutation(), { wrapper });
    act(() => result.current.mutate({ messageId: message.id, isRead: true }));
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledOnce());
    expect(client.getQueryData<InfiniteData<Message[]>>(["starred-messages", "a"])!.pages[0][0].is_read).toBe(true);
    expect(client.getQueryData<Message[]>(["threadMessages", "thread:a"])![0].is_read).toBe(true);
    await act(async () => write.resolve());
  });

  it("keeps an explicit mark-unread while the same detail remains open", async () => {
    const { result } = renderHook(() => ({
      detail: useMessageLoader(message.id, "Strict"),
      flags: useUpdateFlagsMutation(),
    }), { wrapper });
    await waitFor(() => expect(result.current.detail.message?.is_read).toBe(true));
    await act(async () => result.current.flags.mutateAsync({ messageId: message.id, isRead: false }));
    await waitFor(() => expect(result.current.detail.message?.is_read).toBe(false));
    expect(updateMessageFlags).toHaveBeenCalledTimes(2);
    expect(persisted.is_read).toBe(false);
  });

  it("does not consume cached read when a star action cancels the opening metadata fetch", async () => {
    const stale = deferred<Message>();
    const write = deferred<void>();
    client.setQueryData(["message", message.id], { ...message, is_read: true });
    vi.mocked(getMessage).mockImplementationOnce(() => stale.promise);
    const persist = vi.mocked(updateMessageFlags).getMockImplementation()!;
    vi.mocked(updateMessageFlags).mockImplementationOnce(async (...args) => { await write.promise; await persist(...args); });
    const { result } = renderHook(() => ({
      detail: useMessageLoader(message.id, "Strict"), flags: useUpdateFlagsMutation(),
    }), { wrapper });
    await waitFor(() => expect(getMessage).toHaveBeenCalledOnce());
    act(() => result.current.flags.mutate({ messageId: message.id, isStarred: false }));
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(message.id, undefined, false));
    await act(async () => write.resolve());
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(message.id, true, undefined));
    expect(persisted.is_read).toBe(true);
  });

  it("honors manual unread while cached read metadata is still refreshing", async () => {
    const stale = deferred<Message>();
    client.setQueryData(["message", message.id], { ...message, is_read: true });
    vi.mocked(getMessage).mockImplementationOnce(() => stale.promise);
    const { result } = renderHook(() => ({
      detail: useMessageLoader(message.id, "Strict"), flags: useUpdateFlagsMutation(),
    }), { wrapper });
    await waitFor(() => expect(getMessage).toHaveBeenCalledOnce());
    await act(async () => result.current.flags.mutateAsync({ messageId: message.id, isRead: false }));
    await act(async () => stale.resolve({ ...message, is_read: true }));
    await waitFor(() => expect(result.current.detail.message?.is_read).toBe(false));
    expect(updateMessageFlags).toHaveBeenCalledOnce();
  });

  it("does not mark a late metadata response after navigating to another message", async () => {
    const old = deferred<Message>();
    const second = { ...message, id: "a:two" };
    vi.mocked(getMessage).mockImplementation(async (id) => id === message.id ? old.promise : { ...second, is_read: persisted.is_read });
    const { result, rerender } = renderHook(({ id }) => useMessageLoader(id, "Strict"), {
      wrapper, initialProps: { id: message.id },
    });
    rerender({ id: second.id });
    await waitFor(() => expect(result.current.message?.id).toBe(second.id));
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(second.id, true, undefined));
    await act(async () => old.resolve({ ...message }));
    expect(result.current.message?.id).toBe(second.id);
    expect(updateMessageFlags).toHaveBeenCalledTimes(1);
  });

  it("rolls back a failed read without undoing another message or a star change", async () => {
    const failed = deferred<void>();
    const second = { ...message, id: "b:two", account_id: "b" };
    client.setQueryData(["messages", "all:inbox"], {
      pages: [[{ ...message, is_starred: false }, second]], pageParams: [0],
    });
    vi.mocked(updateMessageFlags).mockImplementation((id, read) => id === message.id && read !== undefined ? failed.promise : Promise.resolve());
    const { result } = renderHook(() => useUpdateFlagsMutation(), { wrapper });
    act(() => result.current.mutate({ messageId: message.id, isRead: true }));
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledTimes(1));
    await act(async () => result.current.mutateAsync({ messageId: second.id, isRead: true }));
    await act(async () => result.current.mutateAsync({ messageId: message.id, isStarred: true }));
    await act(async () => failed.reject(new Error("private database path")));
    const cached = client.getQueryData<InfiniteData<Message[]>>(["messages", "all:inbox"])!.pages[0];
    expect(cached[0]).toMatchObject({ is_read: false, is_starred: true });
    expect(cached[1].is_read).toBe(true);
    expect(useToastStore.getState().toasts).toHaveLength(1);
    expect(useToastStore.getState().toasts[0].message).not.toContain("private");
  });

  it("refreshes unread badges after a committed read while an unrelated write is pending", async () => {
    const blocked = deferred<void>();
    vi.mocked(updateMessageFlags).mockImplementation((id) => id === "b:pending" ? blocked.promise : Promise.resolve());
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const { result } = renderHook(() => useUpdateFlagsMutation(), { wrapper });
    act(() => result.current.mutate({ messageId: "b:pending", isRead: true }));
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledOnce());
    await act(async () => result.current.mutateAsync({ messageId: message.id, isRead: true }));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["account-unread-counts"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["folder-unread-counts"] });
    expect(client.isMutating()).toBe(1);
    await act(async () => blocked.resolve());
  });

  it("does not roll a newer same-message flag back when an earlier request fails late", async () => {
    const old = deferred<void>();
    client.setQueryData(["message", message.id], { ...message });
    vi.mocked(updateMessageFlags).mockImplementationOnce(() => old.promise).mockResolvedValue(undefined);
    const { result } = renderHook(() => useUpdateFlagsMutation(), { wrapper });
    act(() => result.current.mutate({ messageId: message.id, isRead: true }));
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledTimes(1));
    await act(async () => result.current.mutateAsync({ messageId: message.id, isRead: false }));
    await act(async () => old.reject(new Error("failed")));
    expect(client.getQueryData<Message>(["message", message.id])!.is_read).toBe(false);
  });

  it("cancels a stale list response before patching it and leaves unrelated metadata requests running", async () => {
    const staleList = deferred<InfiniteData<Message[]>>();
    const otherMessage = deferred<Message>();
    const write = deferred<void>();
    client.setQueryData(["messages", "inbox"], { pages: [[{ ...message }]], pageParams: [0] });
    const list = client.fetchQuery({ queryKey: ["messages", "inbox"], queryFn: () => staleList.promise }).catch(() => undefined);
    const other = client.fetchQuery({ queryKey: ["message", "b:two"], queryFn: () => otherMessage.promise });
    vi.mocked(updateMessageFlags).mockReturnValue(write.promise);
    const { result } = renderHook(() => useUpdateFlagsMutation(), { wrapper });
    act(() => result.current.mutate({ messageId: message.id, isRead: true }));
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledOnce());
    await act(async () => {
      staleList.resolve({ pages: [[{ ...message }]], pageParams: [0] });
      otherMessage.resolve({ ...message, id: "b:two" });
      await Promise.all([list, other]);
    });
    expect(client.getQueryData<InfiniteData<Message[]>>(["messages", "inbox"])!.pages[0][0].is_read).toBe(true);
    expect(client.getQueryData<Message>(["message", "b:two"])!.id).toBe("b:two");
    await act(async () => write.resolve());
  });

  it("leaves a rejected automatic read visible and does not loop on refetch", async () => {
    vi.mocked(updateMessageFlags).mockRejectedValue(new Error("private failure"));
    const { result } = renderHook(() => useMessageLoader(message.id, "Strict"), { wrapper });
    await waitFor(() => expect(useToastStore.getState().toasts).toHaveLength(1));
    await waitFor(() => expect(result.current.message?.is_read).toBe(false));
    expect(result.current.rendered?.html).toContain("Body");
    expect(updateMessageFlags).toHaveBeenCalledOnce();
  });

  it("queues a cached unread message only once under StrictMode effect replay", async () => {
    client.setQueryData(["message", message.id], { ...message });
    const { result } = renderHook(() => useMessageLoader(message.id, "Strict"), {
      wrapper: ({ children }) => <StrictMode><QueryClientProvider client={client}>{children}</QueryClientProvider></StrictMode>,
    });
    await waitFor(() => expect(result.current.message?.is_read).toBe(true));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(updateMessageFlags).toHaveBeenCalledOnce();
  });

  it("refreshes cached read metadata before deciding a reopened message needs no read action", async () => {
    client.setQueryData(["message", message.id], { ...message, is_read: true });
    const { result } = renderHook(() => useMessageLoader(message.id, "Strict"), { wrapper });
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledOnce());
    await waitFor(() => expect(result.current.message?.is_read).toBe(true));
    expect(persisted.is_read).toBe(true);
  });

  it("rejects pre-mutation metadata while read persistence is pending and after it settles", async () => {
    const stale = deferred<Message>();
    const write = deferred<void>();
    client.setQueryData(["message", message.id], { ...message }, { updatedAt: 1 });
    vi.mocked(getMessage).mockImplementationOnce(() => stale.promise)
      .mockImplementation(async () => ({ ...persisted }));
    vi.mocked(updateMessageFlags).mockImplementation(async () => { await write.promise; persisted.is_read = true; });
    const { result } = renderHook(() => useMessageLoader(message.id, "Strict"), { wrapper });
    await waitFor(() => expect(result.current.message?.is_read).toBe(true));
    await act(async () => write.resolve());
    await waitFor(() => expect(getMessage).toHaveBeenCalledTimes(2));
    await act(async () => stale.resolve({ ...message, subject: "STALE" }));
    expect(result.current.message).toMatchObject({ is_read: true, subject: "Unread" });
    expect(updateMessageFlags).toHaveBeenCalledOnce();
  });
});
