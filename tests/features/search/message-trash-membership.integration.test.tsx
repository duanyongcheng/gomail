import "@testing-library/jest-dom/vitest";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider, type InfiniteData } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { call } from "mygo-runtime";
import SearchView from "@/features/search/SearchView";
import StarredView from "@/features/starred/StarredView";
import InboxView from "@/features/inbox/InboxView";
import MessageActionToolbar from "@/components/MessageActionToolbar";
import { listThreadMessages } from "@/lib/api";
import type { Account, Folder, Message } from "@/lib/api";
import i18n from "@/lib/i18n";
import { profileLocalStorage } from "@/lib/profileStorage";
import { useMailStore } from "@/stores/mail.store";
import { useUIStore } from "@/stores/ui.store";
import { useToastStore } from "@/stores/toast.store";
import { useConfirmStore } from "@/stores/confirm.store";

// Keep views, children, hooks, API, command policy/table and generated services real.
// Only the native transport and jsdom's absent virtual-list layout are synthetic.
vi.mock("mygo-runtime", () => ({ call: vi.fn(), on: vi.fn(() => vi.fn()), event: vi.fn() }));
vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getTotalSize: () => count * 76,
    getVirtualItems: () => Array.from({ length: count }, (_, index) => ({ index, key: index, start: index * 76 })),
    measureElement: vi.fn(), measure: vi.fn(), scrollToIndex: vi.fn(),
  }),
}));

const base: Message = {
  id: "a:mail", account_id: "a", remote_id: "mail", message_id_header: null, in_reply_to: null, references_header: null,
  thread_id: "thread:synthetic", subject: "Membership regression", snippet: "Synthetic body", from_address: "sender@example.invalid", from_name: "Sender",
  to_list: [{ name: "Me", address: "me@example.invalid" }], cc_list: [], bcc_list: [], has_attachments: false,
  is_read: true, is_starred: true, is_draft: false, date: 1, remote_version: null, is_deleted: false, deleted_at: null,
  created_at: 1, updated_at: 1, body_text: "Synthetic body", body_html_raw: "", in_trash: false,
};
const accounts: Account[] = ["a", "b"].map((id) => ({ id, email: `${id}@example.invalid`, display_name: id, provider: "gmail", created_at: 1, updated_at: 1 }));
const folders: Folder[] = accounts.flatMap(({ id }) => (["inbox", "trash"] as const).map((role) => ({
  id: `${id}:${role}`, account_id: id, remote_id: role.toUpperCase(), name: role, folder_type: "label", role,
  parent_id: null, color: null, is_system: true, sort_order: 0,
})));
let messages: Message[];
let clients: QueryClient[];

function mount(element: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } } });
  clients.push(client);
  return { client, ...render(<QueryClientProvider client={client}>{element}</QueryClientProvider>) };
}
function methods() { return vi.mocked(call).mock.calls.map(([method]) => method); }
function deleteButton() { return screen.getByRole("button", { name: "Delete", exact: true }); }
function expectNoDelete() {
  expect(methods()).not.toContain("MessageService.Delete");
  expect(methods()).not.toContain("MessageService.BatchDelete");
  expect(useToastStore.getState().toasts).toEqual([]);
}
async function openAlternate(view: "search" | "starred") {
  mount(view === "search" ? <SearchView /> : <StarredView />);
  if (view === "search") {
    const input = screen.getByRole("textbox", { name: "Search", exact: true });
    fireEvent.change(input, { target: { value: "Membership" } });
    fireEvent.submit(input.closest("form")!);
  }
  fireEvent.click(await screen.findByRole("option"));
  await screen.findByRole("region", { name: "Message body" });
  expect(call).toHaveBeenCalledWith("MessageService.Rendered", { messageId: "a:mail", privacyMode: "Strict" });
  if (view === "search") expect(call).toHaveBeenCalledWith("SearchService.Search", { query: "Membership", accountId: "a", limit: 50 });
  else expect(call).toHaveBeenCalledWith("MessageService.List", { folderId: "", accountId: "a", limit: 50, offset: 0, starred: true });
}

beforeEach(async () => {
  vi.clearAllMocks();
  clients = [];
  messages = [{ ...base }];
  await i18n.changeLanguage("en");
  profileLocalStorage.setItem("pebble-privacy-mode", "strict");
  useMailStore.setState({ activeAccountId: "a", activeFolderId: "a:inbox", selectedMessageId: null, selectedThreadId: null, threadView: false, batchMode: false, selectedMessageIds: new Set() });
  useUIStore.setState({ activeView: "search", searchQuery: "", theme: "light" });
  useToastStore.setState({ toasts: [] });
  useConfirmStore.setState({ isOpen: false, resolve: null });
  vi.mocked(call).mockImplementation(async (method, ...args) => {
    const request = args[0] as { accountId?: string; messageId?: string; messageIds?: string[] };
    switch (method) {
      case "AccountService.List": return accounts;
      case "FolderService.List": return folders.filter((f) => f.account_id === request.accountId);
      case "MessageService.List": return messages;
      case "MessageService.Get": return messages.find((m) => m.id === request.messageId);
      case "SearchService.Search": return messages.map((m) => ({ message_id: m.id, subject: m.subject, snippet: m.snippet, score: 1, from_address: m.from_address, date: m.date }));
      case "MessageService.Rendered": return { message: messages.find((m) => m.id === request.messageId), rendered: { html: "<p>Synthetic body</p>", trackers_blocked: [], images_blocked: 0 } };
      case "ThreadService.Messages": return messages.map((m) => ({ ...m, body_text: "", body_html_raw: "" }));
      case "MessageService.Restore":
      case "MessageService.Delete": return {};
      case "MessageService.BatchDelete": return request.messageIds!.length;
      default: throw new Error(`Unexpected native method: ${method}`);
    }
  });
});
afterEach(() => { cleanup(); for (const client of clients) client.clear(); });

describe.each(["search", "starred"] as const)("%s real detail membership", (view) => {
  it("offers restore and blocks no-op deletion of a message already in Trash without a folderRole prop", async () => {
    messages = [{ ...base, in_trash: true }];
    await openAlternate(view);
    expect(deleteButton()).toBeDisabled();
    expect(deleteButton()).toHaveAttribute("title", expect.stringContaining("not available"));
    fireEvent.click(deleteButton());
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expectNoDelete();
    fireEvent.click(screen.getByRole("button", { name: "Restore", exact: true }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("MessageService.Restore", { messageId: "a:mail" }));
    await waitFor(() => expect(screen.queryByRole("region", { name: "Message body" })).not.toBeInTheDocument());
    expect(useToastStore.getState().toasts.map((toast) => toast.message)).toEqual(["Message restored to inbox"]);
  });

  it("retains confirmed move-to-trash for ordinary mail", async () => {
    await openAlternate(view);
    expect(deleteButton()).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Restore", exact: true })).not.toBeInTheDocument();
    fireEvent.click(deleteButton());
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete", exact: true }));
    await waitFor(() => expect(methods().filter((method) => method === "MessageService.Delete")).toHaveLength(1));
    expect(call).toHaveBeenCalledWith("MessageService.Delete", { messageId: "a:mail" });
    expect(useToastStore.getState().toasts.map((toast) => toast.message)).toEqual(["Message deleted"]);
  });
});

it.each([
  { account: "a", folder: "a:inbox", inTrash: true },
  { account: null, folder: "all:inbox", inTrash: true },
  { account: null, folder: "all:trash", inTrash: false },
])("selected membership overrides $folder context ($inTrash)", async ({ account, folder, inTrash }) => {
  messages = [{ ...base, in_trash: inTrash }];
  useMailStore.setState({ activeAccountId: account, activeFolderId: folder });
  mount(<InboxView />);
  fireEvent.click(await screen.findByRole("option"));
  await screen.findByRole("region", { name: "Message body" });
  expect(deleteButton()).toHaveProperty("disabled", inTrash);
  expect(screen.queryByRole("button", { name: "Restore", exact: true }) !== null).toBe(inTrash);
  expectNoDelete();
});

it("preserves membership in metadata-only thread adapters used by message actions", async () => {
  messages = [{ ...base, in_trash: true }];
  const thread = await listThreadMessages("thread:synthetic");
  expect(call).toHaveBeenCalledWith("ThreadService.Messages", { threadId: "thread:synthetic" });
  expect(thread[0]).toMatchObject({ in_trash: true, body_text: "" });
  mount(<MessageActionToolbar message={thread[0]} onBack={vi.fn()} />);
  expect(deleteButton()).toBeDisabled();
  expect(screen.getByRole("button", { name: "Restore", exact: true })).toBeEnabled();
  expectNoDelete();
});

it("guards mixed-account batch selection but permits only the ordinary messages after deselection", async () => {
  messages = [{ ...base }, { ...base, id: "b:mail", account_id: "b", in_trash: true }];
  useMailStore.setState({ activeAccountId: null, activeFolderId: "all:inbox", batchMode: true });
  mount(<InboxView />);
  const rows = await screen.findAllByRole("option");
  fireEvent.click(rows[0]); fireEvent.click(rows[1]);
  expect(deleteButton()).toBeDisabled();
  fireEvent.click(deleteButton());
  expect(useConfirmStore.getState().isOpen).toBe(false);
  expectNoDelete();
  fireEvent.click(rows[1]);
  expect(deleteButton()).toBeEnabled();
  fireEvent.click(deleteButton());
  expect(useConfirmStore.getState().isOpen).toBe(true);
  await act(async () => { useConfirmStore.getState().handleConfirm(); });
  expect(call).toHaveBeenCalledWith("MessageService.BatchDelete", { messageIds: ["a:mail"] });
  expect(methods().filter((method) => method === "MessageService.BatchDelete")).toHaveLength(1);
});

it("rechecks batch membership after confirmation instead of reporting a stale no-op as success", async () => {
  useMailStore.setState({ batchMode: true });
  const { client } = mount(<InboxView />);
  fireEvent.click(await screen.findByRole("option"));
  fireEvent.click(deleteButton());
  expect(useConfirmStore.getState().isOpen).toBe(true);
  await act(async () => {
    client.setQueriesData<InfiniteData<Message[]>>({ queryKey: ["messages"] }, (data) => data && ({ ...data, pages: data.pages.map((page) => page.map((m) => ({ ...m, in_trash: true }))) }));
  });
  await waitFor(() => expect(deleteButton()).toBeDisabled());
  await act(async () => { useConfirmStore.getState().handleConfirm(); });
  expectNoDelete();
  expect(useMailStore.getState().selectedMessageIds).toEqual(new Set(["a:mail"]));
});
