import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MessageSummary } from "@/lib/api";
import { archiveMessage, moveToFolder, updateMessageFlags } from "@/lib/api";
import MessageItem from "@/components/MessageItem";
import { useToastStore } from "@/stores/toast.store";
import "@/lib/i18n";
const mocks = vi.hoisted(() => ({ client: undefined as unknown as QueryClient }));
vi.mock("@tanstack/react-query", async (original) => ({ ...await original<object>(), useQueryClient: () => mocks.client }));
vi.mock("@/lib/api", () => ({ archiveMessage: vi.fn(), moveToFolder: vi.fn(), updateMessageFlags: vi.fn() }));
const base = { id: "one", account_id: "a", subject: "Subject", snippet: "Snippet", from_name: "Sender", from_address: "sender@example.invalid", to_list: [], is_read: true, is_starred: false, has_attachments: false, date: 1 } as MessageSummary;
function mount(extra: Record<string, unknown> = {}, message = base) {
  render(<MessageItem message={message} isSelected={false} onClick={vi.fn()} {...extra} />);
}
function click(label: string) { fireEvent.mouseEnter(screen.getByRole("option")); fireEvent.click(screen.getByRole("button", { name: label, exact: true })); }
beforeEach(() => {
  vi.clearAllMocks(); mocks.client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  mocks.client.setQueryData(["message", base.id], base); useToastStore.setState({ toasts: [] });
  vi.mocked(archiveMessage).mockResolvedValue("archived"); vi.mocked(moveToFolder).mockResolvedValue(); vi.mocked(updateMessageFlags).mockResolvedValue();
});
describe("MessageItem", () => {
  it("labels archive-folder action as unarchive", () => { mount({ folderRole: "archive" }); fireEvent.mouseEnter(screen.getByRole("option")); expect(screen.getByRole("button", { name: "Unarchive" })).toBeTruthy(); });
  it.each(["Archive", "Report spam"])("%s failure leaves rows intact and reports a static error", async (label) => {
    mocks.client.setQueryData(["messages", "inbox"], { pages: [[base]], pageParams: [0] });
    vi.mocked(label === "Archive" ? archiveMessage : moveToFolder).mockRejectedValueOnce(new Error("private path"));
    mount({ spamFolderId: "spam" }); click(label);
    await waitFor(() => expect(useToastStore.getState().toasts.at(-1)?.type).toBe("error"));
    expect(mocks.client.getQueryData(["messages", "inbox"])).toEqual({ pages: [[base]], pageParams: [0] });
    expect(useToastStore.getState().toasts.at(-1)?.message).not.toContain("private");
  });
  it.each(["Archive", "Report spam"])("%s receipt refreshes unread counts", async (label) => {
    const invalidate = vi.spyOn(mocks.client, "invalidateQueries"); mount({ spamFolderId: "spam" }); click(label);
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: ["folder-unread-counts"] }));
  });
  it("row star uses shared all-view refresh", async () => {
    const invalidate = vi.spyOn(mocks.client, "invalidateQueries"); mount(); click("Star");
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(base.id, undefined, true));
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: ["message"] }));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["threadMessages"] });
  });
  it("failed star rolls back only shared owned flags", async () => {
    vi.mocked(updateMessageFlags).mockRejectedValueOnce(new Error("private")); mount(); click("Star");
    await waitFor(() => expect(useToastStore.getState().toasts.at(-1)?.type).toBe("error"));
    expect(mocks.client.getQueryData<MessageSummary>(["message", base.id])?.is_starred).toBe(false);
  });
  it("uses custom batch checkbox and row selection handler", () => {
    const select = vi.fn(); mount({ batchMode: true, batchSelected: false, onToggleBatchSelect: select });
    const checkbox = screen.getByRole("checkbox", { name: "Select message" }); expect(checkbox.className).toContain("message-row-checkbox"); fireEvent.click(checkbox); expect(select).toHaveBeenCalledWith(base.id);
  });
  it("names combined-inbox mailbox with a decorative color bar", () => {
    mount({ accountBadge: { color: "#22c55e", label: "Work", title: "Work · work@example.invalid" } });
    expect(screen.getByTestId("account-badge").textContent).toBe("Work"); expect(screen.getByTestId("account-badge").getAttribute("title")).toContain("work@example.invalid"); expect(screen.getByTestId("account-color-bar").getAttribute("aria-hidden")).toBe("true");
  });
  it("leaves single-mailbox row unlabelled", () => { mount(); expect(screen.queryByTestId("account-badge")).toBeNull(); });
  it("marks unread rows in a separate head gutter", () => {
    mount({}, { ...base, is_read: false }); const row = screen.getByRole("option"); const dot = row.querySelector(".message-row-unread-dot")!;
    expect(row.className).toContain("message-list-row--unread"); expect(dot.getAttribute("aria-hidden")).toBe("true"); expect(dot.parentElement?.className).toContain("message-row-head"); expect(dot.closest(".message-row-sender-name")).toBeNull();
    expect(row.querySelector(".message-row-subject")).toBeTruthy(); expect(row.querySelector(".message-row-date")).toBeTruthy();
  });
  it("read rows have no unread treatment", () => { mount(); expect(screen.getByRole("option").className).not.toContain("message-list-row--unread"); expect(screen.getByRole("option").querySelector(".message-row-unread-dot")).toBeNull(); });
  it("uses theme-driven sizing and decorative sender/row markers", () => {
    mount({ rowIndex: 7 });
    const row = screen.getByRole("option");
    expect(row.style.height).toBe("");
    expect(row.querySelector(".mail-row-avatar")?.getAttribute("aria-hidden")).toBe("true");
    expect(row.querySelector(".mail-row-index")?.textContent).toBe("08");
    expect(row.querySelector(".mail-row-index")?.getAttribute("aria-hidden")).toBe("true");
  });
  it("does not open the message when a nested action receives a keyboard activation", () => {
    const open = vi.fn(); mount({ onClick: open });
    fireEvent.focus(screen.getByRole("option"));
    fireEvent.keyDown(screen.getByRole("button", { name: "Archive" }), { key: "Enter" });
    expect(open).not.toHaveBeenCalled();
    fireEvent.keyDown(screen.getByRole("option"), { key: "Enter" });
    expect(open).toHaveBeenCalledOnce();
  });
  it("sent-folder primary contact uses recipients", () => { mount({ folderRole: "sent" }, { ...base, to_list: [{ name: "Destination", address: "to@example.invalid" }] }); expect(screen.getByText("Destination")).toBeTruthy(); expect(screen.queryByText("Sender")).toBeNull(); });
});
