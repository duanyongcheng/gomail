import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider, type InfiniteData, onlineManager } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { call } from "mygo-runtime";
import InboxView from "@/features/inbox/InboxView";
import MessageActionToolbar from "@/components/MessageActionToolbar";
import MarkAllReadButton from "@/components/MarkAllReadButton";
import { useConfirmStore } from "@/stores/confirm.store";
import StarredView from "@/features/starred/StarredView";
import Sidebar from "@/components/Sidebar";
import SearchView from "@/features/search/SearchView";
import { useUIStore } from "@/stores/ui.store";
import { executeMessageFlags, messageFlagsMutationKey, refreshMessageFlagViews } from "@/hooks/mutations/messageFlagsMutation";
import { manualUnreadVersion } from "@/hooks/queries/messageFlagsCache";
import { useMailStore } from "@/stores/mail.store";
import { useToastStore } from "@/stores/toast.store";
import type { Account, Folder, Message } from "@/lib/api";
import i18n from "@/lib/i18n";

// Real views, QueryClient, API adapter and generated bindings; only native IPC
// and missing jsdom list geometry are simulated.
vi.mock("mygo-runtime", () => ({ call: vi.fn(), on: vi.fn(() => vi.fn()), event: vi.fn() }));
vi.mock("@tanstack/react-virtual", () => ({ useVirtualizer: ({ count }: { count: number }) => ({
  getTotalSize: () => count * 76,
  getVirtualItems: () => Array.from({ length: count }, (_, index) => ({ index, key: index, start: index * 76 })),
  measureElement: vi.fn(), scrollToIndex: vi.fn(),
}) }));
const account: Account = { id: "a", email: "a@example.invalid", display_name: "A", provider: "gmail", created_at: 1, updated_at: 1 };
const folder: Folder = { id: "a:inbox", account_id: "a", remote_id: "INBOX", name: "Inbox", folder_type: "label", role: "inbox", parent_id: null, color: null, is_system: true, sort_order: 0 };
const base: Message = {
  id: "a:one", account_id: "a", remote_id: "one", message_id_header: null, in_reply_to: null, references_header: null,
  thread_id: "thread:a", subject: "One", snippet: "Synthetic", from_address: "sender@example.invalid", from_name: "Sender",
  to_list: [], cc_list: [], bcc_list: [], has_attachments: false, is_read: true, is_starred: false, is_draft: false,
  date: 1, remote_version: null, is_deleted: false, deleted_at: null, created_at: 1, updated_at: 1,
  body_text: "Body", body_html_raw: "", in_trash: false, capabilities: { modify_flags: true },
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
let client: QueryClient;
let persisted: Message[];
let handlers: Map<string, (request: any) => Promise<unknown>>;
let online: boolean;
const detailKey = ["message", base.id] as const;
const listKey = ["messages", folder.id, undefined] as const;
const starKey = ["starred-messages", "a"] as const;
const threadKey = ["threadMessages", "thread:a"] as const;
const t = (key: string, fallback: string) => i18n.t(key, fallback);
function row(id = base.id) { return screen.getAllByRole("option").find((node) => node.textContent?.includes(id === base.id ? "One" : "Two"))!; }
function clickRow(label: string, id = base.id) { const node = row(id); fireEvent.mouseEnter(node); fireEvent.click(within(node).getByRole("button", { name: label, exact: true })); }
function writes(method: string) { return vi.mocked(call).mock.calls.filter(([name]) => name === method); }
function listMessage(id = base.id) { return client.getQueryData<InfiniteData<Message[]>>(listKey)!.pages.flat().find((m) => m.id === id)!; }
function seed() {
  client.setQueryData(detailKey, { ...persisted[0] }, { updatedAt: 1 });
  client.setQueryData(threadKey, structuredClone(persisted), { updatedAt: 1 });
  client.setQueryData(starKey, { pages: [structuredClone(persisted)], pageParams: [0] }, { updatedAt: 1 });
}
function mount(extra?: React.ReactNode) { return render(<QueryClientProvider client={client}><InboxView />{extra}</QueryClientProvider>); }
beforeEach(async () => {
  vi.clearAllMocks(); await i18n.changeLanguage("en");
  online = onlineManager.isOnline(); onlineManager.setOnline(true);
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  persisted = [{ ...base }, { ...base, id: "a:two", remote_id: "two", subject: "Two" }]; handlers = new Map();
  useMailStore.setState({ activeAccountId: "a", activeFolderId: folder.id, selectedMessageId: null, selectedThreadId: null, threadView: false, batchMode: false, selectedMessageIds: new Set() });
  useToastStore.setState({ toasts: [] });
  useUIStore.setState({ activeView: "inbox", sidebarCollapsed: false, showFolderUnreadCount: true, searchQuery: "" });
  vi.mocked(call).mockImplementation(async (method, ...args) => {
    const request = args[0] as any;
    if (handlers.has(method)) return handlers.get(method)!(request);
    switch (method) {
      case "AccountService.List": return [account];
      case "FolderService.List": return [folder];
      case "MessageService.List": return structuredClone(persisted);
      case "MessageService.Get": return structuredClone(persisted.find((m) => m.id === request.messageId));
      case "MessageService.Flags": {
        const m = persisted.find((m) => m.id === request.messageId)!;
        if (request.isRead != null) m.is_read = request.isRead;
        if (request.isStarred != null) m.is_starred = request.isStarred;
        return {};
      }
      case "MessageService.Archive": return "archived";
      case "MessageService.BatchFlags": return request.messageIds.length;
      case "MessageService.MarkAccountAllRead": return 2;
      case "MessageService.UnreadCounts": return { [request.byFolder ? folder.id : "a"]: 2 };
      case "MessageService.MarkFolderAllRead": return 2;
      case "SearchService.Search": return persisted.map((m) => ({ message_id: m.id, subject: m.subject, snippet: m.snippet, from_address: m.from_address, date: m.date, score: 1 }));
      default: throw new Error(`Unexpected native method ${method}`);
    }
  });
});
afterEach(() => { cleanup(); client.clear(); onlineManager.setOnline(online); });

describe("supported list actions share flag ownership", () => {
  it("row star resolves freshest intent and updates detail/thread without promoting stale read metadata", async () => {
    const star = deferred<void>();
    handlers.set("MessageService.Flags", () => star.promise);
    seed(); persisted[0].is_starred = true;
    mount(); await screen.findAllByRole("option");
    const timestamp = client.getQueryState(threadKey)!.dataUpdatedAt;
    clickRow("Unstar");
    await waitFor(() => expect(writes("MessageService.Flags")).toHaveLength(1));
    expect(client.isMutating({ mutationKey: messageFlagsMutationKey })).toBe(1);
    expect(client.getQueryData<Message>(detailKey)?.is_starred).toBe(false);
    expect(client.getQueryData<Message[]>(threadKey)?.[0].is_starred).toBe(false);
    expect(client.getQueryState(threadKey)!.dataUpdatedAt).toBe(timestamp);
    await act(async () => star.resolve());
  });

  it("archive failure never restores whole rows over newer same-message and unrelated flag successes", async () => {
    seed(); const archive = deferred<string>(); const hold = deferred<void>();
    handlers.set("MessageService.Archive", () => archive.promise);
    handlers.set("MessageService.Flags", async (request) => {
      if (request.messageId === "a:hold") await hold.promise;
      else { const m = persisted.find((m) => m.id === request.messageId)!; if (request.isStarred != null) m.is_starred = request.isStarred; if (request.isRead != null) m.is_read = request.isRead; }
      return {};
    });
    mount(); await screen.findAllByRole("option");
    const staleList = deferred<Message[]>();
    handlers.set("MessageService.List", () => staleList.promise);
    clickRow("Archive");
    await waitFor(() => expect(writes("MessageService.Archive")).toHaveLength(1));
    const holding = executeMessageFlags(client, { messageId: "a:hold", isRead: true }, t);
    await act(async () => {
      await executeMessageFlags(client, { messageId: base.id, isStarred: true }, t);
      await executeMessageFlags(client, { messageId: "a:two", isRead: false }, t);
    });
    await act(async () => archive.reject(new Error("private sqlite path")));
    await waitFor(() => expect(useToastStore.getState().toasts.some((toast) => toast.type === "error")).toBe(true));
    expect(listMessage().is_starred).toBe(true);
    expect(listMessage("a:two").is_read).toBe(false);
    expect(client.getQueryData<Message>(detailKey)?.is_starred).toBe(true);
    await act(async () => { hold.resolve(); staleList.resolve(structuredClone(persisted)); await holding; });
  });

  it("batch unread enters the same pending barrier and records explicit unread before receipt", async () => {
    seed(); const batch = deferred<number>(); handlers.set("MessageService.BatchFlags", () => batch.promise);
    useMailStore.setState({ batchMode: true, selectedMessageIds: new Set([base.id, "a:two"]) });
    mount(); await screen.findAllByRole("option");
    fireEvent.click(screen.getByRole("button", { name: "Mark as unread", exact: true }));
    await waitFor(() => expect(writes("MessageService.BatchFlags")).toHaveLength(1));
    expect(client.isMutating({ mutationKey: messageFlagsMutationKey })).toBe(1);
    expect(manualUnreadVersion(client, base.id)).toBeDefined();
    const reads = writes("MessageService.List").length;
    act(() => refreshMessageFlagViews(client));
    await act(async () => {});
    expect(writes("MessageService.List")).toHaveLength(reads);
    // A partial receipt identifies no individual modified IDs; don't guess.
    expect(listMessage().is_read).toBe(true);
    await act(async () => batch.resolve(1));
    await waitFor(() => expect(client.isMutating()).toBe(0));
    expect(useToastStore.getState().toasts.at(-1)?.message).toContain("1");
    expect([...useMailStore.getState().selectedMessageIds]).toEqual([base.id, "a:two"]);
  });

  it("rapid row and toolbar toggles share intent, and an old failure cannot erase the newer read/star", async () => {
    seed(); const old = deferred<void>(); const newer = deferred<void>();
    let starCalls = 0;
    handlers.set("MessageService.Flags", async (request) => {
      if (request.isStarred != null) await (++starCalls === 1 ? old.promise : newer.promise);
      const m = persisted.find((m) => m.id === request.messageId)!;
      if (request.isRead != null) m.is_read = request.isRead;
      if (request.isStarred != null) m.is_starred = request.isStarred;
      return {};
    });
    mount(<div data-testid="toolbar"><MessageActionToolbar message={base} onBack={vi.fn()} /></div>);
    await screen.findAllByRole("option"); clickRow("Star");
    await waitFor(() => expect(writes("MessageService.Flags")).toHaveLength(1));
    // Toolbar deliberately holds the original prop: intent comes from cache,
    // not that stale render. Read is a third overlapping field writer.
    fireEvent.click(within(screen.getByTestId("toolbar")).getByRole("button", { name: "Star", exact: true }));
    await act(async () => { await executeMessageFlags(client, { messageId: base.id, isRead: false }, t); });
    await waitFor(() => expect(writes("MessageService.Flags")).toHaveLength(3));
    expect(writes("MessageService.Flags").filter(([, request]) => (request as any).isStarred != null).map(([, request]) => (request as any).isStarred)).toEqual([true, false]);
    await act(async () => newer.resolve());
    await act(async () => old.reject(new Error("older star failed")));
    await waitFor(() => expect(client.isMutating()).toBe(0));
    expect(listMessage()).toMatchObject({ is_read: false, is_starred: false });
    expect(client.getQueryData<Message>(detailKey)).toMatchObject({ is_read: false, is_starred: false });
  });

  it("skipped row/toolbar archive retains rows, detail and truthful receipts", async () => {
    seed(); const archive = deferred<string>(); handlers.set("MessageService.Archive", () => archive.promise);
    const back = vi.fn(); mount(<div data-testid="toolbar"><MessageActionToolbar message={base} onBack={back} /></div>);
    await screen.findAllByRole("option"); clickRow("Archive");
    await waitFor(() => expect(writes("MessageService.Archive")).toHaveLength(1));
    fireEvent.click(within(screen.getByTestId("toolbar")).getByRole("button", { name: "Archive", exact: true }));
    await waitFor(() => expect(client.isMutating()).toBe(1));
    expect(back).not.toHaveBeenCalled(); expect(listMessage().id).toBe(base.id);
    expect(useToastStore.getState().toasts).toHaveLength(0);
    await act(async () => archive.resolve("skipped"));
    await waitFor(() => expect(client.isMutating()).toBe(0));
    expect(back).not.toHaveBeenCalled(); expect(listMessage().id).toBe(base.id);
    expect(useToastStore.getState().toasts).toHaveLength(0);
  });

  it("batch double-click submits once and does not clear a newer selection after full acknowledgement", async () => {
    const batch = deferred<number>(); handlers.set("MessageService.BatchFlags", () => batch.promise);
    useMailStore.setState({ batchMode: true, selectedMessageIds: new Set([base.id, "a:two"]) });
    mount(); await screen.findAllByRole("option");
    const button = screen.getByRole("button", { name: "Star", exact: true });
    act(() => { fireEvent.click(button); fireEvent.click(button); });
    await waitFor(() => expect(writes("MessageService.BatchFlags")).toHaveLength(1));
    act(() => useMailStore.getState().selectAllMessages(["a:two"]));
    await act(async () => batch.resolve(2));
    await waitFor(() => expect(client.isMutating()).toBe(0));
    expect([...useMailStore.getState().selectedMessageIds]).toEqual(["a:two"]);
  });

  it("delete rechecks selection after confirmation and never submits stale IDs", async () => {
    const confirmation = deferred<boolean>(); const confirm = vi.fn(() => confirmation.promise);
    useConfirmStore.setState({ confirm });
    useMailStore.setState({ batchMode: true, selectedMessageIds: new Set([base.id]) });
    mount(); await screen.findAllByRole("option"); fireEvent.click(screen.getByRole("button", { name: "Delete", exact: true }));
    expect(confirm).toHaveBeenCalledOnce();
    act(() => useMailStore.getState().selectAllMessages(["a:two"]));
    await act(async () => confirmation.resolve(true));
    expect(writes("MessageService.BatchDelete")).toHaveLength(0);
    expect([...useMailStore.getState().selectedMessageIds]).toEqual(["a:two"]);
  });

  it("toolbar archive acknowledgement does not navigate away from a newer message", async () => {
    const archive = deferred<string>(); handlers.set("MessageService.Archive", () => archive.promise);
    const back = vi.fn();
    const view = (message: Message) => <QueryClientProvider client={client}><MessageActionToolbar message={message} onBack={back} /></QueryClientProvider>;
    const { rerender } = render(view(base)); fireEvent.click(screen.getByRole("button", { name: "Archive", exact: true }));
    await waitFor(() => expect(writes("MessageService.Archive")).toHaveLength(1));
    rerender(view(persisted[1])); await act(async () => archive.resolve("archived"));
    await waitFor(() => expect(client.isMutating()).toBe(0)); expect(back).not.toHaveBeenCalled();
  });

  it("mark-all runs offline, is guarded against rapid clicks and waits behind pending flags for list refresh", async () => {
    seed(); const all = deferred<number>(); const star = deferred<void>();
    handlers.set("MessageService.MarkAccountAllRead", () => all.promise); handlers.set("MessageService.Flags", () => star.promise);
    mount(<MarkAllReadButton accountId="a" accountLabel="A" unread={2} />); await screen.findAllByRole("option");
    onlineManager.setOnline(false); clickRow("Star");
    await waitFor(() => expect(writes("MessageService.Flags")).toHaveLength(1));
    const button = screen.getByTestId("mark-all-read-a"); act(() => { fireEvent.click(button); fireEvent.click(button); });
    await waitFor(() => expect(writes("MessageService.MarkAccountAllRead")).toHaveLength(1));
    const reads = writes("MessageService.List").length;
    await act(async () => all.resolve(0));
    expect(client.isMutating()).toBe(1); expect(listMessage().is_starred).toBe(true); expect(writes("MessageService.List")).toHaveLength(reads);
    expect(useToastStore.getState().toasts.at(-1)?.message).toContain("0");
    await act(async () => { persisted[0].is_starred = true; star.resolve(); });
    await waitFor(() => expect(client.isMutating()).toBe(0));
    await waitFor(() => expect(writes("MessageService.List")).toHaveLength(reads + 1));
  });

  it("sidebar folder mark-all shares the mutation barrier while local acknowledgement is deferred", async () => {
    const all = deferred<number>(); handlers.set("MessageService.MarkFolderAllRead", () => all.promise);
    render(<QueryClientProvider client={client}><Sidebar /></QueryClientProvider>);
    const folderRow = await screen.findByTestId(`folder-row-${folder.id}`);
    fireEvent.contextMenu(folderRow, { clientX: 20, clientY: 20 });
    const button = await screen.findByRole("menuitem", { name: "Mark all as read" });
    await waitFor(() => expect(button).not.toBeDisabled()); fireEvent.click(button);
    await waitFor(() => expect(writes("MessageService.MarkFolderAllRead")).toHaveLength(1));
    expect(writes("MessageService.MarkFolderAllRead")[0][1]).toEqual({ accountId: "a", folderIds: [folder.id] });
    expect(client.isMutating({ mutationKey: messageFlagsMutationKey })).toBe(1);
    await act(async () => all.resolve(2));
    await waitFor(() => expect(client.isMutating()).toBe(0));
    expect(useToastStore.getState().toasts.at(-1)?.message).toContain("2");
  });

  it("basic SearchView executes uncached local reads and receipt refetch while browser is offline", async () => {
    onlineManager.setOnline(false); const star = deferred<void>(); handlers.set("MessageService.Flags", () => star.promise);
    render(<QueryClientProvider client={client}><SearchView /></QueryClientProvider>);
    const input = screen.getByRole("textbox", { name: "Search", exact: true });
    fireEvent.change(input, { target: { value: "One" } }); fireEvent.submit(input.closest("form")!);
    await screen.findAllByRole("option"); expect(writes("SearchService.Search")).toHaveLength(1);
    const done = executeMessageFlags(client, { messageId: base.id, isStarred: true }, t);
    await waitFor(() => expect(writes("MessageService.Flags")).toHaveLength(1));
    act(() => refreshMessageFlagViews(client)); expect(writes("SearchService.Search")).toHaveLength(1);
    await act(async () => { star.resolve(); await done; });
    await waitFor(() => expect(writes("SearchService.Search")).toHaveLength(2));
  });

  it("starred view derives hidden rows from shared flags and brings them back on rollback", async () => {
    persisted[0].is_starred = true; const star = deferred<void>(); handlers.set("MessageService.Flags", () => star.promise);
    render(<QueryClientProvider client={client}><StarredView /></QueryClientProvider>);
    await screen.findAllByRole("option"); clickRow("Unstar");
    await waitFor(() => expect(writes("MessageService.Flags")).toHaveLength(1));
    expect(screen.queryByRole("option")).toBeNull();
    await act(async () => star.reject(new Error("failed local write")));
    await screen.findByRole("option"); expect(client.getQueryData<InfiniteData<Message[]>>(starKey)?.pages.flat()[0].is_starred).toBe(true);
  });
});
