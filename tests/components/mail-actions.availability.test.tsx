import "@testing-library/jest-dom/vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { Message } from "@/lib/api";
import MessageActionToolbar from "@/components/MessageActionToolbar";
import MessageList from "@/components/MessageList";
import MessageItem from "@/components/MessageItem";
import MessageDetail from "@/components/MessageDetail";
import ThreadMessageBubble from "@/components/ThreadMessageBubble";
import FolderContextMenu from "@/components/FolderContextMenu";
import SnoozePopover from "@/features/inbox/SnoozePopover";
import { MessageService } from "@/mygo";
import * as platform from "@/lib/platform/core";
import { commands } from "@/lib/platform/manifest";
import { useMailStore } from "@/stores/mail.store";
import { useComposeStore } from "@/stores/compose.store";
import { useUIStore } from "@/stores/ui.store";
import { useConfirmStore } from "@/stores/confirm.store";

// Production components -> API -> real command table. Only the native RPC boundary is synthetic.
vi.mock("@/mygo", () => ({ MessageService: { archive: vi.fn(), delete: vi.fn(), restore: vi.fn(), batchDelete: vi.fn(), rendered: vi.fn(), get: vi.fn(), flags: vi.fn() } }));
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string | { defaultValue?: string }) => typeof fallback === "string" ? fallback : fallback?.defaultValue ?? key }) }));
vi.mock("@tanstack/react-virtual", () => ({ useVirtualizer: ({ count }: { count: number }) => ({ getTotalSize: () => count * 76, getVirtualItems: () => Array.from({ length: count }, (_, index) => ({ index, key: index, start: index * 76 })), measureElement: vi.fn(), measure: vi.fn(), scrollToIndex: vi.fn() }) }));

const message: Message = {
  id: "m", account_id: "a", remote_id: "remote", message_id_header: null, in_reply_to: null, references_header: null,
  thread_id: null, subject: "Synthetic mail", snippet: "Synthetic body", from_address: "sender@example.com", from_name: "Sender",
  to_list: [{ name: "Me", address: "me@example.com" }], cc_list: [], bcc_list: [], has_attachments: false,
  is_read: true, is_starred: false, is_draft: false, date: 1, remote_version: null, is_deleted: false, deleted_at: null,
  created_at: 1, updated_at: 1, body_text: "Synthetic body", body_html_raw: "",
};
function mount(element: React.ReactNode, role: "inbox" | "trash" = "inbox") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(["accounts"], [{ id: "a", email: "me@example.com", provider: "gmail" }]);
  client.setQueryData(["folders", "a"], [{ id: "f", account_id: "a", role, name: role, sort_order: 0 }]);
  return { client, ...render(<QueryClientProvider client={client}>{element}</QueryClientProvider>) };
}
beforeEach(() => {
  vi.restoreAllMocks();
  vi.clearAllMocks();
  useMailStore.setState({ activeAccountId: "a", activeFolderId: "f", selectedMessageIds: new Set(["m"]), batchMode: true });
  useComposeStore.setState({ composeMode: null, composeReplyTo: null, composeDirty: false });
  useUIStore.setState({ activeView: "inbox", pendingRuleDraftText: null });
  vi.mocked(MessageService.archive).mockResolvedValue("archived");
  vi.mocked(MessageService.delete).mockResolvedValue({});
  vi.mocked(MessageService.restore).mockResolvedValue({});
  vi.mocked(MessageService.batchDelete).mockResolvedValue(1);
  vi.mocked(MessageService.get).mockResolvedValue(message);
  vi.mocked(MessageService.rendered).mockResolvedValue({ message, rendered: { html: "<p>Synthetic body</p>", mode: "Strict", trackers_blocked: [], images_blocked: 0, styles_blocked: 0 } });
});

it("disables detail Kanban but preserves confirmed move-to-trash and reply through production paths", async () => {
  const invoke = vi.spyOn(platform, "invoke");
  mount(<MessageActionToolbar message={message} onBack={vi.fn()} />);
  const kanban = screen.getByRole("button", { name: "messageActions.addToKanban" });
  expect(kanban).toBeDisabled();
  expect(kanban.getAttribute("title")).toMatch(/not available/);
  fireEvent.click(kanban);
  expect(invoke).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "messageActions.reply" }));
  expect(useComposeStore.getState().composeReplyTo).toEqual(message);
  fireEvent.click(screen.getByRole("button", { name: "messageActions.delete" }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }));
  await waitFor(() => expect(MessageService.delete).toHaveBeenCalledExactlyOnceWith({ messageId: "m" }));
});

it("blocks trash deletion but still restores with the actual command adapter", async () => {
  mount(<MessageActionToolbar message={message} folderRole="trash" onBack={vi.fn()} />);
  const remove = screen.getByRole("button", { name: "messageActions.delete" });
  expect(remove).toBeDisabled();
  fireEvent.click(remove);
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(MessageService.delete).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Restore" }));
  await waitFor(() => expect(MessageService.restore).toHaveBeenCalledExactlyOnceWith({ messageId: "m" }));
});

it.each(["inbox", "trash"] as const)("batch %s avoids local label queries and only permits supported deletion", async (role) => {
  const invoke = vi.spyOn(platform, "invoke");
  const confirm = vi.spyOn(useConfirmStore.getState(), "confirm").mockResolvedValue(true);
  mount(<MessageList messages={[message]} selectedMessageId={null} onSelectMessage={vi.fn()} loading={false} />, role);
  const remove = screen.getByRole("button", { name: "common.delete" });
  expect((remove as HTMLButtonElement).disabled).toBe(role === "trash");
  fireEvent.click(remove);
  if (role === "inbox") await waitFor(() => expect(MessageService.batchDelete).toHaveBeenCalledExactlyOnceWith({ messageIds: ["m"] }));
  else { expect(confirm).not.toHaveBeenCalled(); expect(MessageService.batchDelete).not.toHaveBeenCalled(); }
  expect(invoke.mock.calls.some(([name]) => name === "get_message_labels_batch")).toBe(false);
});

it("row actions used by inbox and starred keep archive but cannot add Kanban cards", async () => {
  const invoke = vi.spyOn(platform, "invoke");
  mount(<MessageItem message={message} isSelected={false} onClick={vi.fn()} />);
  fireEvent.mouseEnter(screen.getByRole("option"));
  const kanban = screen.getByRole("button", { name: "messageActions.addToKanban" });
  expect(kanban).toBeDisabled(); fireEvent.click(kanban);
  expect(invoke).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Archive" }));
  await waitFor(() => expect(MessageService.archive).toHaveBeenCalledExactlyOnceWith({ messageId: "m" }));
});

it("real detail guards direct AI/translation events without pending command calls", async () => {
  const invoke = vi.spyOn(platform, "invoke");
  mount(<MessageDetail messageId="m" onBack={vi.fn()} />);
  await screen.findByRole("region", { name: "Message body" });
  for (const name of ["Snooze message", "Toggle bilingual view", "ai.summarize"]) {
    const button = screen.getByRole("button", { name });
    expect(button).toBeDisabled(); fireEvent.click(button);
  }
  await act(async () => {
    for (const event of ["gomail:translate-selection", "gomail:toggle-bilingual", "gomail:ai-summarize"]) document.dispatchEvent(new CustomEvent(event));
  });
  expect(invoke.mock.calls.map(([name]) => name)).toEqual(["get_message", "get_rendered_html"]);
  expect(screen.queryByText("ai.summaryTitle")).toBeNull();
});

it("expanded threads cannot access contacts but still fetch full content and reply", async () => {
  const invoke = vi.spyOn(platform, "invoke");
  mount(<ThreadMessageBubble message={{ ...message, body_text: "" }} defaultExpanded />);
  await waitFor(() => expect(MessageService.rendered).toHaveBeenCalled());
  const contact = screen.getByRole("button", { name: "Add sender@example.com to contacts" });
  expect(contact).toBeDisabled(); fireEvent.click(contact);
  expect(screen.queryByRole("dialog")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "messageActions.reply" }));
  await waitFor(() => expect(useComposeStore.getState().composeReplyTo?.body_text).toBe("Synthetic body"));
  // Expansion now reads fresh local metadata independently of HTML; replying
  // still explicitly fetches the complete current message before composing.
  expect(invoke.mock.calls.map(([name]) => name)).toEqual(["get_message", "get_rendered_html", "get_message"]);
});

it("even a directly mounted snooze popover cannot run the pending command", () => {
  const invoke = vi.spyOn(platform, "invoke");
  const onSnoozed = vi.fn();
  mount(<SnoozePopover messageId="m" onClose={vi.fn()} onSnoozed={onSnoozed} />);
  for (const button of screen.getAllByRole("button")) {
    expect(button).toBeDisabled();
    expect(button.getAttribute("title")).toMatch(/not available/);
    fireEvent.click(button);
  }
  expect(invoke).not.toHaveBeenCalled();
  expect(onSnoozed).not.toHaveBeenCalled();
});

it("folder menus deny permanent delete despite the implemented command marker, including keyboard activation", () => {
  const empty = vi.fn(); const mark = vi.fn();
  expect(commands.empty_trash.status).toBe("implemented");
  mount(<FolderContextMenu position={{ x: 1, y: 1 }} label="Trash actions" busy={false} onClose={vi.fn()} items={[
    { id: "empty-trash", label: "Empty trash", icon: null, onSelect: empty },
    { id: "empty-spam", label: "Empty spam", icon: null, onSelect: empty },
    { id: "mark-all-read", label: "Mark all read", icon: null, onSelect: mark },
  ]} />);
  for (const name of ["Empty trash", "Empty spam"]) {
    const button = screen.getByRole("menuitem", { name });
    expect(button).toBeDisabled();
    expect(button.getAttribute("title")).toMatch(/not available/);
    fireEvent.click(button); fireEvent.keyDown(button, { key: "Enter" });
  }
  expect(empty).not.toHaveBeenCalled();
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Mark all read" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "Mark all read" }));
  expect(mark).toHaveBeenCalledOnce();
});
