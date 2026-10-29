import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Account } from "@/lib/api";
import type { GmailLabelsView } from "@/mygo";

const mocks = vi.hoisted(() => ({ list: vi.fn(), change: vi.fn(), confirm: vi.fn() }));
vi.mock("@/mygo", () => ({ GmailLabelService: { list: mocks.list, change: mocks.change } }));
vi.mock("@/stores/confirm.store", () => ({ useConfirmStore: { getState: () => ({ confirm: mocks.confirm }) } }));
vi.mock("@tauri-apps/api/event", () => ({ listen: () => Promise.resolve(() => {}) }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (_key: string, fallback: string) => fallback }) }));
import GmailLabels from "@/features/settings/GmailLabels";

const accounts: Account[] = ["a", "b"].map((id) => ({ id, email: `${id}@example.invalid`, display_name: id, provider: "gmail", created_at: 1, updated_at: 1 }));
function view(account = "a", name = "Work"): GmailLabelsView {
  return { pending: null, folders: [
    { id: `${account}-inbox`, account_id: account, remote_id: "INBOX", name: "Inbox", is_system: true, folder_type: "label", role: "inbox", parent_id: null, color: null, sort_order: 0 },
    { id: `${account}-work`, account_id: account, remote_id: "Label_work", name, is_system: false, folder_type: "label", role: null, parent_id: null, color: null, sort_order: 0 },
  ] };
}
let client: QueryClient;
function show() { return render(<QueryClientProvider client={client}><GmailLabels accounts={accounts} /></QueryClientProvider>); }
beforeEach(() => {
  vi.resetAllMocks();
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mocks.list.mockImplementation(async ({ accountId }) => view(accountId));
  mocks.change.mockResolvedValue(view()); mocks.confirm.mockResolvedValue(true);
});
afterEach(() => { cleanup(); client.clear(); });

describe("Gmail server label settings", () => {
  it("loads locally and exposes no system edit/delete actions", async () => {
    show(); await screen.findByText("Work");
    expect(mocks.change).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Delete: Inbox" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Rename: Inbox" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Refresh from Gmail" }));
    await waitFor(() => expect(mocks.change).toHaveBeenCalledWith(expect.objectContaining({ accountId: "a", action: "refresh" })));
  });
  it("creates and renames a custom label with busy protection", async () => {
    let finish!: (value: GmailLabelsView) => void;
    mocks.change.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    show(); await screen.findByText("Work");
    fireEvent.change(screen.getByRole("textbox", { name: "New label name" }), { target: { value: "New" } });
    fireEvent.click(screen.getByRole("button", { name: "Create", exact: true }));
    expect((screen.getByRole("button", { name: "Create", exact: true }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Create", exact: true }));
    expect(mocks.change).toHaveBeenCalledTimes(1);
    await act(async () => finish(view()));
    await waitFor(() => expect((screen.getByRole("button", { name: "Rename: Work" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Rename: Work" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Rename label" }), { target: { value: "Renamed" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mocks.change).toHaveBeenLastCalledWith(expect.objectContaining({ action: "rename", folderId: "a-work", name: "Renamed" })));
  });
  it("requires confirmation and explains that deletion does not delete mail", async () => {
    mocks.confirm.mockResolvedValueOnce(false);
    show(); await screen.findByText("Work");
    fireEvent.click(screen.getByRole("button", { name: "Delete: Work" }));
    await waitFor(() => expect(mocks.confirm).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining("No messages are deleted") })));
    await waitFor(() => expect((screen.getByRole("button", { name: "Delete: Work" }) as HTMLButtonElement).disabled).toBe(false));
    expect(mocks.change).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Delete: Work" }));
    await waitFor(() => expect(mocks.change).toHaveBeenCalledWith(expect.objectContaining({ action: "delete", accountId: "a", folderId: "a-work" })));
  });
  it("blocks mutations for uncertain results but allows read-only refresh", async () => {
    mocks.list.mockResolvedValue({ ...view(), pending: { id: "pending-1", action: "create", name: "New", remoteId: "", createdAt: 1 } });
    show(); await screen.findByText(/previous label change needs confirmation/);
    expect((screen.getByRole("button", { name: "Rename: Work" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Refresh from Gmail" }));
    await waitFor(() => expect(mocks.change).toHaveBeenCalledWith(expect.objectContaining({ action: "refresh" })));
  });
  it("ignores the old account result after switching accounts", async () => {
    let finish!: (value: GmailLabelsView) => void;
    mocks.change.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    show(); await screen.findByText("Work");
    fireEvent.click(screen.getByRole("button", { name: "Refresh from Gmail" }));
    fireEvent.change(screen.getByRole("combobox", { name: "Label account" }), { target: { value: "b" } });
    await waitFor(() => expect(mocks.list).toHaveBeenCalledWith({ accountId: "b" }));
    await act(async () => finish(view("a", "Stale label")));
    expect(screen.queryByText("Stale label")).toBeNull();
    expect(screen.getByRole("button", { name: "Rename: Work" })).toBeTruthy();
  });
  it("shows read and write failures instead of reporting success", async () => {
    mocks.list.mockRejectedValueOnce(new Error("disk"));
    show(); expect(await screen.findByRole("alert")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByText("Work");
    mocks.change.mockRejectedValueOnce(new Error("Refresh before retrying"));
    fireEvent.click(screen.getByRole("button", { name: "Refresh from Gmail" }));
    expect((await screen.findByRole("alert")).textContent).toContain("Refresh before retrying");
  });
});
