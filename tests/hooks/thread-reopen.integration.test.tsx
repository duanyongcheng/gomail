import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, onlineManager } from "@tanstack/react-query";
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ThreadMessageBubble from "@/components/ThreadMessageBubble";
import { useThreadMessagesQuery } from "@/hooks/queries/useThreadMessagesQuery";
import { executeMessageFlags } from "@/hooks/mutations/messageFlagsMutation";
import { getMessage, getRenderedHtml, listThreadMessages, updateMessageFlags } from "@/lib/api";
import type { Message, RenderedHtml } from "@/lib/api";

vi.mock("@/lib/api", () => ({ getMessage: vi.fn(), getRenderedHtml: vi.fn(), listThreadMessages: vi.fn(), updateMessageFlags: vi.fn() }));
vi.mock("@tauri-apps/api/event", () => ({ listen: vi.fn(async () => () => {}) }));
vi.mock("@/components/AttachmentList", () => ({ default: () => null }));
vi.mock("@/components/ContactAddressAction", () => ({ default: () => null }));
vi.mock("@/components/ShadowDomEmail", () => ({ ShadowDomEmail: () => null }));
vi.mock("@/lib/i18n", () => ({ default: { t: (_key: string, fallback: string) => fallback } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (_key: string, fallback: string) => fallback }) }));

const base = {
  id: "a:one", account_id: "a", from_address: "sender@example.invalid", from_name: "Sender",
  to_list: [], cc_list: [], bcc_list: [], is_read: true, is_starred: false,
  updated_at: 1, date: 1, capabilities: { modify_flags: true },
} as Message;
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => { resolve = yes; });
  return { promise, resolve };
}
const t = (_key: string, fallback: string) => fallback;
let client: QueryClient;
let persisted: Message;
function Thread({ id = "thread:a", expanded = true }: { id?: string; expanded?: boolean }) {
  const query = useThreadMessagesQuery(id);
  return <>{query.data?.map((message) => <ThreadMessageBubble key={message.id} message={message} defaultExpanded={expanded} />)}</>;
}
function mount(props: { id?: string; expanded?: boolean } = {}) {
  return render(<Thread {...props} />, {
    wrapper: ({ children }) => <StrictMode><QueryClientProvider client={client}>{children}</QueryClientProvider></StrictMode>,
  });
}
beforeEach(() => {
  vi.resetAllMocks();
  client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 30_000 }, mutations: { retry: false } } });
  persisted = { ...base };
  vi.mocked(getMessage).mockImplementation(async (id) => ({ ...persisted, id }));
  vi.mocked(listThreadMessages).mockImplementation(async () => [{ ...persisted }]);
  vi.mocked(getRenderedHtml).mockReturnValue(new Promise<RenderedHtml>(() => {}));
  vi.mocked(updateMessageFlags).mockImplementation(async (_id, read, star) => {
    if (read !== undefined) persisted.is_read = read;
    if (star !== undefined) persisted.is_starred = star;
  });
});
afterEach(() => { cleanup(); client.clear(); onlineManager.setOnline(true); });

describe("fresh local metadata for each thread opening", () => {
  it("opens and acknowledges the fresh local conversation while offline", async () => {
    client.setQueryData(["threadMessages", "thread:a"], [{ ...base }]);
    client.setQueryData(["message", base.id], { ...base });
    persisted.is_read = false;
    onlineManager.setOnline(false);
    mount();
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(base.id, true, undefined));
    expect(listThreadMessages).toHaveBeenCalled();
    expect(getMessage).toHaveBeenCalled();
    await waitFor(() => expect(client.isMutating()).toBe(0));
    expect(persisted.is_read).toBe(true);
  });

  it.each([1, Date.now()])("does not consume cached read before deferred fresh unread, cache timestamp %s", async (updatedAt) => {
    const thread = deferred<Message[]>();
    const detail = deferred<Message>();
    client.setQueryData(["threadMessages", "thread:a"], [{ ...base }], { updatedAt });
    client.setQueryData(["message", base.id], { ...base }, { updatedAt });
    persisted.is_read = false;
    vi.mocked(listThreadMessages).mockImplementationOnce(() => thread.promise);
    vi.mocked(getMessage).mockImplementationOnce(() => detail.promise);
    mount();
    await waitFor(() => expect(listThreadMessages).toHaveBeenCalled());
    expect(getMessage).toHaveBeenCalled();
    expect(updateMessageFlags).not.toHaveBeenCalled();
    await act(async () => thread.resolve([{ ...persisted }]));
    expect(updateMessageFlags).not.toHaveBeenCalled();
    await act(async () => detail.resolve({ ...persisted }));
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(base.id, true, undefined));
    await waitFor(() => expect(client.isMutating()).toBe(0));
    expect(client.getQueryData<Message[]>(["threadMessages", "thread:a"])![0].is_read).toBe(true);
    expect(updateMessageFlags).toHaveBeenCalledOnce();
    // HTML is still blocked; it does not participate in the opening decision.
    expect(screen.getByRole("status")).toBeTruthy();
  });

  it("marks cached unread exactly once under StrictMode and rejects a stale metadata response", async () => {
    const old = deferred<Message>();
    const write = deferred<void>();
    persisted.is_read = false;
    client.setQueryData(["threadMessages", "thread:a"], [{ ...persisted }]);
    client.setQueryData(["message", base.id], { ...persisted });
    vi.mocked(getMessage).mockImplementationOnce(() => old.promise);
    vi.mocked(updateMessageFlags).mockImplementation(async () => { await write.promise; persisted.is_read = true; });
    mount();
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledOnce());
    await act(async () => old.resolve({ ...base, is_read: false }));
    expect(client.getQueryData<Message>(["message", base.id])?.is_read).toBe(true);
    await act(async () => write.resolve());
    await waitFor(() => expect(client.isFetching()).toBe(0));
    expect(updateMessageFlags).toHaveBeenCalledOnce();
  });

  it("keeps manual unread through refetch, then refreshes again on re-expansion and reopening within 30s", async () => {
    const view = mount();
    await waitFor(() => expect(getMessage).toHaveBeenCalled());
    await waitFor(() => expect(client.isFetching()).toBe(0));
    await act(async () => { await executeMessageFlags(client, { messageId: base.id, isRead: false }, t); });
    await waitFor(() => expect(client.isFetching()).toBe(0));
    expect(updateMessageFlags).toHaveBeenCalledTimes(1);
    expect(persisted.is_read).toBe(false);

    fireEvent.click(screen.getByRole("button", { expanded: true }));
    const beforeExpand = vi.mocked(getMessage).mock.calls.length;
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledTimes(2));
    expect(vi.mocked(getMessage).mock.calls.length).toBeGreaterThan(beforeExpand);
    await waitFor(() => expect(client.isMutating()).toBe(0));
    view.unmount();

    // External local update, while both read=true caches are still "fresh".
    persisted.is_read = false;
    const beforeOpen = vi.mocked(listThreadMessages).mock.calls.length;
    mount();
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledTimes(3));
    expect(vi.mocked(listThreadMessages).mock.calls.length).toBeGreaterThan(beforeOpen);
    expect(persisted.is_read).toBe(true);
  });

  it("does not consume cached read when a star action cancels the initial local refresh", async () => {
    const detail = deferred<Message>();
    const write = deferred<void>();
    client.setQueryData(["threadMessages", "thread:a"], [{ ...base }]);
    client.setQueryData(["message", base.id], { ...base });
    persisted.is_read = false;
    vi.mocked(getMessage).mockImplementationOnce(() => detail.promise);
    const persist = vi.mocked(updateMessageFlags).getMockImplementation()!;
    vi.mocked(updateMessageFlags).mockImplementationOnce(async (...args) => { await write.promise; await persist(...args); });
    mount();
    await waitFor(() => expect(getMessage).toHaveBeenCalledOnce());
    let star!: Promise<void>;
    act(() => { star = executeMessageFlags(client, { messageId: base.id, toggleStarred: true }, t); });
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(base.id, undefined, true));
    await act(async () => { write.resolve(); await star; });
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(base.id, true, undefined));
    expect(persisted.is_read).toBe(true);
  });

  it("honors manual unread even while the initial read=true metadata refresh is pending", async () => {
    const detail = deferred<Message>();
    client.setQueryData(["threadMessages", "thread:a"], [{ ...base }]);
    client.setQueryData(["message", base.id], { ...base });
    vi.mocked(getMessage).mockImplementationOnce(() => detail.promise);
    mount();
    await waitFor(() => expect(getMessage).toHaveBeenCalledOnce());
    await act(async () => { await executeMessageFlags(client, { messageId: base.id, isRead: false }, t); });
    await act(async () => detail.resolve({ ...base }));
    await waitFor(() => expect(client.isFetching()).toBe(0));
    expect(updateMessageFlags).toHaveBeenCalledOnce();
    expect(persisted.is_read).toBe(false);
    expect(client.getQueryData<Message>(["message", base.id])?.is_read).toBe(false);
  });

  it("re-arms after an explicit unread action performed while collapsed", async () => {
    mount();
    await waitFor(() => expect(getMessage).toHaveBeenCalled());
    await waitFor(() => expect(client.isFetching()).toBe(0));
    fireEvent.click(screen.getByRole("button", { expanded: true }));
    await act(async () => { await executeMessageFlags(client, { messageId: base.id, isRead: false }, t); });
    await waitFor(() => expect(client.isFetching()).toBe(0));
    expect(updateMessageFlags).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledTimes(2));
    expect(persisted.is_read).toBe(true);
  });

  it("ignores delayed old-thread metadata after navigating to a different message", async () => {
    const old = deferred<Message>();
    client.setQueryData(["threadMessages", "thread:a"], [{ ...base }]);
    client.setQueryData(["message", base.id], { ...base });
    vi.mocked(getMessage).mockImplementation((id) => id === base.id ? old.promise : Promise.resolve({ ...base, id }));
    vi.mocked(listThreadMessages).mockImplementation(async (id) => [{ ...base, id: id === "thread:a" ? base.id : "b:two" }]);
    const view = mount();
    await waitFor(() => expect(getMessage).toHaveBeenCalledWith(base.id));
    view.rerender(<Thread id="thread:b" />);
    await waitFor(() => expect(getMessage).toHaveBeenCalledWith("b:two"));
    await act(async () => old.resolve({ ...base, is_read: false }));
    expect(updateMessageFlags).not.toHaveBeenCalled();
  });

  it("does not fetch or mark collapsed messages, and ignores a result arriving after collapse", async () => {
    const detail = deferred<Message>();
    client.setQueryData(["threadMessages", "thread:a"], [{ ...base }]);
    vi.mocked(getMessage).mockReturnValue(detail.promise);
    mount({ expanded: false });
    await waitFor(() => expect(listThreadMessages).toHaveBeenCalled());
    expect(getMessage).not.toHaveBeenCalled();
    expect(getRenderedHtml).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    await waitFor(() => expect(getMessage).toHaveBeenCalled());
    fireEvent.click(screen.getByRole("button", { expanded: true }));
    await act(async () => detail.resolve({ ...base, is_read: false }));
    expect(updateMessageFlags).not.toHaveBeenCalled();
  });
});
