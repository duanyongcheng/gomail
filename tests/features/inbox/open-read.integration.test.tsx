import "@testing-library/jest-dom/vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { call } from "mygo-runtime";
import InboxView from "@/features/inbox/InboxView";
import SearchView from "@/features/search/SearchView";
import StarredView from "@/features/starred/StarredView";
import { invalidateUnreadViews, useAccountUnreadCounts } from "@/hooks/queries";
import { useFolderUnreadCounts } from "@/hooks/queries/useFolderUnreadCounts";
import type { Account, Folder, Message } from "@/lib/api";
import { updateMessageFlags } from "@/lib/api";
import i18n from "@/lib/i18n";
import { profileLocalStorage } from "@/lib/profileStorage";
import { useMailStore } from "@/stores/mail.store";
import { useUIStore } from "@/stores/ui.store";
import { useToastStore } from "@/stores/toast.store";

// Exercise real views, hooks, typed command adapters and generated bindings.
// Only the native transport and jsdom's missing list geometry are simulated.
vi.mock("mygo-runtime", () => ({ call: vi.fn(), on: vi.fn(() => vi.fn()), event: vi.fn() }));
vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getTotalSize: () => count * 76,
    getVirtualItems: () => Array.from({ length: count }, (_, index) => ({ index, key: index, start: index * 76 })),
    measureElement: vi.fn(), scrollToIndex: vi.fn(),
  }),
}));

const account: Account = { id: "a", email: "a@example.invalid", display_name: "Account", provider: "gmail", created_at: 1, updated_at: 1 };
const folder: Folder = {
  id: "a:inbox", account_id: "a", remote_id: "INBOX", name: "Inbox", folder_type: "label", role: "inbox",
  parent_id: null, color: null, is_system: true, sort_order: 0,
};
const base: Message = {
  id: "a:mail", account_id: "a", remote_id: "mail", message_id_header: null,
  in_reply_to: null, references_header: null, thread_id: "thread:a",
  subject: "Unread regression", snippet: "Synthetic body", from_address: "sender@example.invalid", from_name: "Latest",
  to_list: [{ name: "Me", address: "a@example.invalid" }], cc_list: [], bcc_list: [], has_attachments: false,
  is_read: false, is_starred: true, is_draft: false, date: 1, remote_version: null, is_deleted: false, deleted_at: null,
  created_at: 1, updated_at: 1, body_text: "Synthetic body", body_html_raw: "", in_trash: false,
  capabilities: { modify_flags: true },
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
let messages: Message[];
let client: QueryClient;
let body: ReturnType<typeof deferred<void>>;
let write: ReturnType<typeof deferred<void>>;
function nativeWrites() { return vi.mocked(call).mock.calls.filter(([method]) => method === "MessageService.Flags"); }
function Counts() {
  const accounts = useAccountUnreadCounts();
  const folders = useFolderUnreadCounts("a");
  return <div><output data-testid="account-unread">{accounts.a ?? -1}</output><output data-testid="folder-unread">{folders.data?.["a:inbox"] ?? -1}</output></div>;
}
function mount(view: "inbox" | "search" | "starred" | "thread") {
  useMailStore.setState({ threadView: view === "thread" });
  return render(<QueryClientProvider client={client}>
    {view === "search" ? <SearchView /> : view === "starred" ? <StarredView /> : <InboxView />}
    <Counts />
  </QueryClientProvider>);
}

beforeEach(async () => {
  vi.clearAllMocks();
  await i18n.changeLanguage("en");
  profileLocalStorage.setItem("pebble-privacy-mode", "strict");
  useMailStore.setState({ activeAccountId: "a", activeFolderId: "a:inbox", selectedMessageId: null, selectedThreadId: null, threadView: false, batchMode: false, selectedMessageIds: new Set() });
  useUIStore.setState({ activeView: "inbox", searchQuery: "", theme: "light", showFolderUnreadCount: true });
  useToastStore.setState({ toasts: [] });
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  messages = [{ ...base }];
  body = deferred<void>();
  write = deferred<void>();
  vi.mocked(call).mockImplementation(async (method, ...args) => {
    const request = args[0] as { messageId?: string; isRead?: boolean | null; isStarred?: boolean | null; byFolder?: boolean };
    switch (method) {
      case "AccountService.List": return [account];
      case "FolderService.List": return [folder];
      case "MessageService.List": return structuredClone(messages);
      case "MessageService.Get": return structuredClone(messages.find((m) => m.id === request.messageId));
      case "MessageService.Rendered":
        await body.promise;
        return { message: structuredClone(messages.find((m) => m.id === request.messageId)), rendered: { html: "<p>Synthetic body</p>", trackers_blocked: [], images_blocked: 0 } };
      case "MessageService.Flags": {
        await write.promise;
        const message = messages.find((m) => m.id === request.messageId)!;
        if (request.isRead != null) message.is_read = request.isRead;
        if (request.isStarred != null) message.is_starred = request.isStarred;
        return {};
      }
      case "MessageService.UnreadCounts": return { [request.byFolder ? "a:inbox" : "a"]: messages.filter((m) => !m.is_read).length };
      case "SearchService.Search": return messages.map((m) => ({ message_id: m.id, subject: m.subject, snippet: m.snippet, score: 1, from_address: m.from_address, date: m.date }));
      case "ThreadService.Messages": return structuredClone(messages).map((m) => ({ ...m, body_text: "", body_html_raw: "" }));
      case "ThreadService.List": return [{
        thread_id: "thread:a", account_id: "a", subject: base.subject, snippet: base.snippet,
        last_date: 1, message_count: messages.length, unread_count: messages.filter((m) => !m.is_read).length,
        is_starred: true, participants: messages.map((m) => m.from_name), has_attachments: false,
      }];
      default: throw new Error(`Unexpected native method: ${method}`);
    }
  });
});
afterEach(() => { cleanup(); client.clear(); });

describe.each(["inbox", "search", "starred", "thread"] as const)("open read through %s", (view) => {
  it("marks read before slow body rendering and refreshes both counts on local acknowledgement", async () => {
    mount(view);
    if (view === "search") {
      const input = screen.getByRole("textbox", { name: "Search", exact: true });
      fireEvent.change(input, { target: { value: "Unread" } });
      fireEvent.submit(input.closest("form")!);
    }
    await waitFor(() => expect(screen.getByTestId("account-unread")).toHaveTextContent("1"));
    fireEvent.click(await screen.findByRole("option"));
    await waitFor(() => expect(nativeWrites()).toHaveLength(1));
    expect(nativeWrites()[0]).toEqual(["MessageService.Flags", { messageId: base.id, isRead: true, isStarred: null }]);
    if (view === "inbox" || view === "starred") expect(screen.getByRole("option")).not.toHaveClass("message-list-row--unread");
    expect(screen.getByTestId("account-unread")).toHaveTextContent("1");
    expect(screen.queryByRole("region", { name: "Message body" })).not.toBeInTheDocument();
    await act(async () => write.resolve());
    await waitFor(() => expect(screen.getByTestId("account-unread")).toHaveTextContent("0"));
    expect(screen.getByTestId("folder-unread")).toHaveTextContent("0");
    if (view === "thread") await waitFor(() => expect(screen.getByRole("option")).not.toHaveClass("thread-list-row--unread"));
    expect(messages[0].is_read).toBe(true);
    expect(screen.queryByRole("region", { name: "Message body" })).not.toBeInTheDocument();
    await act(async () => body.resolve());
    if (view === "thread") {
      await waitFor(() => expect(Array.from(document.querySelectorAll("div")).some((element) => element.shadowRoot?.textContent?.includes("Synthetic body"))).toBe(true));
    } else {
      await screen.findByRole("region", { name: "Message body" });
    }
    expect(nativeWrites()).toHaveLength(1);
  });
});

it("does not mark collapsed thread messages read until explicitly expanded", async () => {
  messages = [{ ...base, id: "a:earlier", remote_id: "earlier", from_name: "Earlier" }, { ...base }];
  mount("thread");
  fireEvent.click(await screen.findByRole("option"));
  await waitFor(() => expect(nativeWrites()).toHaveLength(1));
  expect(nativeWrites()[0][1]).toMatchObject({ messageId: base.id });
  await act(async () => write.resolve());
  await waitFor(() => expect(screen.getByTestId("account-unread")).toHaveTextContent("1"));
  expect(messages[0].is_read).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: /Earlier/, expanded: false }));
  await waitFor(() => expect(nativeWrites()).toHaveLength(2));
  await waitFor(() => expect(screen.getByTestId("account-unread")).toHaveTextContent("0"));
  expect(messages.every((m) => m.is_read)).toBe(true);
  await act(async () => body.resolve());
});

it("restores unread styling and reports a sanitized failure if local persistence fails", async () => {
  mount("inbox");
  fireEvent.click(await screen.findByRole("option"));
  await waitFor(() => expect(nativeWrites()).toHaveLength(1));
  await act(async () => write.reject(new Error("private local database path")));
  await waitFor(() => expect(screen.getByRole("option")).toHaveClass("message-list-row--unread"));
  expect(messages[0].is_read).toBe(false);
  expect(screen.getByTestId("account-unread")).toHaveTextContent("1");
  expect(useToastStore.getState().toasts.map((toast) => toast.message)).toEqual(["Could not update or open this message."]);
  await act(async () => body.resolve());
  await screen.findByRole("region", { name: "Message body" });
  expect(nativeWrites()).toHaveLength(1);
});

it("does not let a delayed toolbar star receipt replace a newer read state", async () => {
  mount("inbox");
  fireEvent.click(await screen.findByRole("option"));
  await waitFor(() => expect(nativeWrites()).toHaveLength(1));
  await act(async () => { write.resolve(); body.resolve(); });
  await screen.findByRole("region", { name: "Message body" });
  await waitFor(() => expect(client.getQueryData<Message>(["message", base.id])?.is_read).toBe(true));

  const star = deferred<void>();
  const native = vi.mocked(call).getMockImplementation()!;
  vi.mocked(call).mockImplementation(async (method, ...args) => {
    const request = args[0] as { isStarred?: boolean | null };
    if (method === "MessageService.Flags" && request.isStarred != null) {
      await star.promise;
      messages[0].is_starred = request.isStarred;
      return {};
    }
    return native(method, ...args);
  });
  fireEvent.click(screen.getByRole("button", { name: "Unstar", exact: true }));
  await waitFor(() => expect(nativeWrites()).toHaveLength(2));

  // Other entries, such as the command palette, persist and invalidate directly.
  await act(async () => {
    await updateMessageFlags(base.id, false);
    invalidateUnreadViews(client);
  });
  await waitFor(() => expect(client.getQueryData<Message>(["message", base.id])?.is_read).toBe(false));
  await act(async () => star.resolve());
  await waitFor(() => expect(client.getQueryData<Message>(["message", base.id])?.is_starred).toBe(false));
  expect(messages[0].is_read).toBe(false);
  expect(client.getQueryData<Message>(["message", base.id])?.is_read).toBe(false);
});
