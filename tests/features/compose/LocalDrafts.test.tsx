import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const mocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), remove: vi.fn(), open: vi.fn(), confirm: vi.fn(), cloud: vi.fn(), listRemote: vi.fn(), importRemote: vi.fn() }));
vi.mock("@/lib/platform/compose", () => ({ listLocalDrafts: mocks.list, getLocalDraft: mocks.get, changeCloudDraft: mocks.cloud, listGmailDrafts: mocks.listRemote, importGmailDraft: mocks.importRemote }));
vi.mock("@/lib/api", () => ({ deleteDraft: mocks.remove }));
vi.mock("@tauri-apps/api/event", () => ({ listen: async () => () => {} }));
vi.mock("@/stores/compose.store", () => ({ useComposeStore: { getState: () => ({ openCompose: mocks.open }) } }));
vi.mock("@/stores/confirm.store", () => ({ useConfirmStore: { getState: () => ({ confirm: mocks.confirm }) } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (_key: string, fallback: string) => fallback }) }));
import LocalDrafts from "@/features/compose/LocalDrafts";
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.list.mockResolvedValue([{ id: "d", accountId: "a", subject: "Saved draft", updatedAt: 1, attachmentCount: 1 }]);
  mocks.get.mockResolvedValue({ id: "d", accountId: "a", subject: "Saved draft" });
  mocks.remove.mockResolvedValue({}); mocks.confirm.mockResolvedValue(true);
  mocks.cloud.mockReset(); mocks.cloud.mockResolvedValue({ state: "synced" });
  mocks.listRemote.mockReset(); mocks.listRemote.mockResolvedValue({ items: [{ id: "remote-d", messageId: "remote-m", subject: "Web draft", updatedAt: 1, localDraftId: "" }], nextPageToken: "" });
  mocks.importRemote.mockReset(); mocks.importRemote.mockResolvedValue({ id: "imported", accountId: "a", subject: "Web draft", bcc: ["hidden@example.com"], attachments: [{ name: "file.txt", path: "opaque", size: 1 }] });
});
describe("local drafts", () => {
  it("restores only after fetching the account-scoped complete draft", async () => {
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    fireEvent.click(await screen.findByRole("button", { name: "Saved draft" }));
    await waitFor(() => expect(mocks.open).toHaveBeenCalledWith("new", null, { localDraft: { id: "d", accountId: "a", subject: "Saved draft" } }));
    expect(mocks.get).toHaveBeenCalledWith("a", "d");
    expect(screen.getByText(/not synced to Gmail/)).toBeTruthy();
    expect(mocks.listRemote).not.toHaveBeenCalled();
  });
  it("does not replace a dirty compose and deletes with confirmation", async () => {
    const view = render(<LocalDrafts accountId="a" disabled />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    expect((await screen.findByRole("button", { name: "Saved draft" }) as HTMLButtonElement).disabled).toBe(true);
    view.rerender(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Delete: Saved draft" }));
    await waitFor(() => expect(mocks.remove).toHaveBeenCalledWith("a", "d"));
  });
  it("does not delete if compose protection changes during confirmation", async () => {
    let confirm!: (confirmed: boolean) => void;
    mocks.confirm.mockReturnValueOnce(new Promise<boolean>((resolve) => { confirm = resolve; }));
    const view = render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    fireEvent.click(await screen.findByRole("button", { name: "Delete: Saved draft" }));
    view.rerender(<LocalDrafts accountId="a" disabled />);
    await act(async () => { confirm(true); });
    expect(mocks.remove).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Saved draft" })).toBeTruthy();
  });
  it("uploads only on explicit action and scopes cloud requests", async () => {
    mocks.list.mockResolvedValue([{ id: "d", accountId: "a", subject: "Saved draft", updatedAt: 1, cloudAvailable: true, cloudState: "local" }]);
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    const upload = await screen.findByRole("button", { name: "Upload to Gmail" });
    expect(mocks.cloud).not.toHaveBeenCalled();
    fireEvent.click(upload);
    await waitFor(() => expect(mocks.cloud).toHaveBeenCalledWith("a", "d", "upload"));
  });
  it("blocks unknown uploads and deletion but allows a read-only check", async () => {
    mocks.list.mockResolvedValue([{ id: "d", accountId: "a", subject: "Saved draft", updatedAt: 1, cloudAvailable: true, cloudState: "unknown" }]);
    mocks.cloud.mockRejectedValueOnce(new Error("Check inconclusive"));
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    expect((await screen.findByRole("button", { name: "Update Gmail copy" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Delete: Saved draft" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Check Gmail copy" }));
    await waitFor(() => expect(mocks.cloud).toHaveBeenCalledWith("a", "d", "check"));
    expect((await screen.findByRole("alert")).textContent).toContain("Check inconclusive");
  });
  it("hides old-account rows immediately while the next account loads", async () => {
    const view = render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    await screen.findByRole("button", { name: "Saved draft" });
    mocks.list.mockReturnValueOnce(new Promise(() => {}));
    view.rerender(<LocalDrafts accountId="b" disabled={false} />);
    expect(screen.queryByRole("button", { name: "Saved draft" })).toBeNull();
    expect(mocks.cloud).not.toHaveBeenCalled();
  });
  it("shows read errors and supports retry", async () => {
    mocks.list.mockRejectedValueOnce(new Error("locked"));
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    expect(await screen.findByRole("alert")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("button", { name: "Saved draft" })).toBeTruthy();
  });
  it("browses only on explicit Gmail action and imports the listed version with its complete saved contents", async () => {
    render(<LocalDrafts accountId="a" disabled={false} />);
    expect(mocks.listRemote).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Gmail drafts" }));
    fireEvent.click(await screen.findByRole("button", { name: "Import draft: Web draft" }));
    await waitFor(() => expect(mocks.importRemote).toHaveBeenCalledWith("a", "remote-d", "remote-m"));
    expect(mocks.open).toHaveBeenCalledWith("new", null, { localDraft: expect.objectContaining({ id: "imported", bcc: ["hidden@example.com"], attachments: [{ name: "file.txt", path: "opaque", size: 1 }] }) });
    expect(mocks.cloud).not.toHaveBeenCalled();
    expect(mocks.list).not.toHaveBeenCalled();
  });
  it("paginates the current account without losing earlier rows and refreshes from the first page", async () => {
    mocks.listRemote.mockResolvedValueOnce({ items: [{ id: "d1", messageId: "m1", subject: "First", updatedAt: 0, localDraftId: "" }], nextPageToken: "next" });
    mocks.listRemote.mockResolvedValueOnce({ items: [{ id: "d1", messageId: "m1", subject: "First", updatedAt: 0, localDraftId: "" }, { id: "d2", messageId: "m2", subject: "Second", updatedAt: 0, localDraftId: "" }], nextPageToken: "" });
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Gmail drafts" }));
    fireEvent.click(await screen.findByRole("button", { name: "Load more drafts" }));
    await screen.findByRole("button", { name: "Import draft: Second" });
    expect(screen.getAllByRole("button", { name: "Import draft: First" })).toHaveLength(1);
    expect(mocks.listRemote.mock.calls).toEqual([["a", ""], ["a", "next"]]);
    expect(screen.queryByRole("button", { name: "Load more drafts" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Refresh Gmail drafts" }));
    await screen.findByRole("button", { name: "Import draft: Web draft" });
    expect(screen.queryByText("First")).toBeNull();
    expect(mocks.listRemote).toHaveBeenLastCalledWith("a", "");
  });
  it("leaves local drafts available while a Gmail read is stalled and ignores its late response", async () => {
    let resolve!: (page: unknown) => void;
    mocks.listRemote.mockReturnValueOnce(new Promise((r) => { resolve = r; }));
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Gmail drafts" }));
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    await screen.findByRole("button", { name: "Saved draft" });
    await act(async () => { resolve({ items: [{ id: "remote", subject: "Late remote" }], nextPageToken: "" }); });
    expect(screen.queryByText("Late remote")).toBeNull();
    expect(screen.getByRole("button", { name: "Saved draft" })).toBeTruthy();
  });
  it("does not request or display another mailbox when account changes during a remote read", async () => {
    let resolve!: (page: unknown) => void;
    mocks.listRemote.mockReturnValueOnce(new Promise((r) => { resolve = r; }));
    const view = render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Gmail drafts" }));
    view.rerender(<LocalDrafts accountId="b" disabled={false} />);
    await act(async () => { resolve({ items: [{ id: "remote", subject: "Account A" }], nextPageToken: "" }); });
    expect(screen.queryByText("Account A")).toBeNull();
    expect(mocks.listRemote.mock.calls).toEqual([["a", ""]]);
    fireEvent.click(screen.getByRole("button", { name: "Gmail drafts" }));
    await screen.findByRole("button", { name: "Import draft: Web draft" });
    expect(mocks.listRemote).toHaveBeenLastCalledWith("b", "");
  });
  it("preserves intervening edits when an import completes after the compose becomes protected", async () => {
    let resolve!: (draft: unknown) => void;
    mocks.importRemote.mockReturnValueOnce(new Promise((r) => { resolve = r; }));
    const view = render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Gmail drafts" }));
    fireEvent.click(await screen.findByRole("button", { name: "Import draft: Web draft" }));
    view.rerender(<LocalDrafts accountId="a" disabled />);
    await act(async () => { resolve({ id: "imported", accountId: "a" }); });
    expect(mocks.open).not.toHaveBeenCalled();
    expect(screen.getByRole("status").textContent).toContain("Your current edits were not replaced");
  });
  it("does not restore an import after changing account or unmounting", async () => {
    let resolve!: (draft: unknown) => void;
    mocks.importRemote.mockReturnValueOnce(new Promise((r) => { resolve = r; }));
    const view = render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Gmail drafts" }));
    fireEvent.click(await screen.findByRole("button", { name: "Import draft: Web draft" }));
    view.rerender(<LocalDrafts accountId="b" disabled={false} />);
    view.unmount();
    await act(async () => { resolve({ id: "imported", accountId: "a" }); });
    expect(mocks.open).not.toHaveBeenCalled();
  });
  it("opens an already imported local draft instead of overwriting it from Gmail", async () => {
    mocks.listRemote.mockResolvedValue({ items: [{ id: "remote-d", messageId: "remote-m", subject: "Web draft", updatedAt: 1, localDraftId: "d" }], nextPageToken: "" });
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Gmail drafts" }));
    fireEvent.click(await screen.findByRole("button", { name: "Open local draft: Web draft" }));
    await waitFor(() => expect(mocks.get).toHaveBeenCalledWith("a", "d"));
    expect(mocks.importRemote).not.toHaveBeenCalled();
    expect(mocks.open).toHaveBeenCalled();
  });
  it("retries only the failed page without exposing native error details", async () => {
    mocks.listRemote.mockRejectedValueOnce(new Error("private-url-and-token"));
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Gmail drafts" }));
    expect((await screen.findByRole("alert")).textContent).toContain("Could not load Gmail drafts");
    expect(screen.queryByText(/private-url-and-token/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByRole("button", { name: "Import draft: Web draft" });
    expect(mocks.listRemote.mock.calls).toEqual([["a", ""], ["a", ""]]);
  });
  it("confirms irreversible Gmail deletion without deleting the local draft", async () => {
    mocks.list.mockResolvedValue([{ id: "d", accountId: "a", subject: "Saved draft", updatedAt: 1, cloudAvailable: true, cloudState: "synced" }]);
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    fireEvent.click(await screen.findByRole("button", { name: "Delete Gmail copy" }));
    await waitFor(() => expect(mocks.cloud).toHaveBeenCalledWith("a", "d", "delete"));
    expect(mocks.confirm).toHaveBeenCalledWith(expect.objectContaining({ destructive: true, title: "Delete Gmail draft permanently?", message: expect.stringContaining("cannot be undone") }));
    expect(mocks.remove).not.toHaveBeenCalled();
  });
  it("rechecks account and dirty state after Gmail deletion confirmation", async () => {
    let confirm!: (result: boolean) => void;
    mocks.confirm.mockReturnValueOnce(new Promise((resolve) => { confirm = resolve; }));
    mocks.list.mockResolvedValue([{ id: "d", accountId: "a", subject: "Saved draft", updatedAt: 1, cloudAvailable: true, cloudState: "synced" }]);
    const view = render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    fireEvent.click(await screen.findByRole("button", { name: "Delete Gmail copy" }));
    view.rerender(<LocalDrafts accountId="b" disabled />);
    await act(async () => { confirm(true); });
    expect(mocks.cloud).not.toHaveBeenCalled();
  });
  it("recovers conflict by keeping both versions and opens only the returned local draft", async () => {
    mocks.list.mockResolvedValue([{ id: "d", accountId: "a", subject: "Saved draft", updatedAt: 1, cloudAvailable: true, cloudState: "conflict" }]);
    mocks.cloud.mockResolvedValue({ state: "synced", draftId: "new-local" });
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    fireEvent.click(await screen.findByRole("button", { name: "Keep both versions" }));
    await waitFor(() => expect(mocks.get).toHaveBeenCalledWith("a", "new-local"));
    expect(mocks.cloud).toHaveBeenCalledWith("a", "d", "import-remote");
    expect(mocks.confirm).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining("Nothing in Gmail is overwritten") }));
    expect(mocks.remove).not.toHaveBeenCalled();
  });
  it("requires explicit confirmation to detach a known conflict without a remote write", async () => {
    mocks.list.mockResolvedValue([{ id: "d", accountId: "a", subject: "Saved draft", updatedAt: 1, cloudAvailable: true, cloudState: "conflict" }]);
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    fireEvent.click(await screen.findByRole("button", { name: "Keep local version" }));
    await waitFor(() => expect(mocks.cloud).toHaveBeenCalledWith("a", "d", "keep-local"));
    expect(mocks.confirm).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining("Gmail is not changed") }));
    expect(mocks.remove).not.toHaveBeenCalled();
    expect(mocks.get).not.toHaveBeenCalled();
  });
  it.each(["unknown", "deleting", "sending"])("does not enable destructive recovery or duplicate sending for %s state", async state => {
    mocks.list.mockResolvedValue([{ id: "d", accountId: "a", subject: "Saved draft", updatedAt: 1, cloudAvailable: true, cloudState: state }]);
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    const remove = await screen.findByRole("button", { name: "Delete: Saved draft" });
    expect((remove as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByRole("button", { name: "Keep local version" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Keep both versions" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete Gmail copy" })).toBeNull();
    if (state === "sending") {
      expect((screen.getByRole("button", { name: "Saved draft" }) as HTMLButtonElement).disabled).toBe(true);
      expect(screen.getByText(/Check this send in Outbox/)).toBeTruthy();
    } else expect((screen.getByRole("button", { name: "Update Gmail copy" }) as HTMLButtonElement).disabled).toBe(true);
  });
  it("opens the Gmail browser when explicitly requested by the selected account's Drafts folder", async () => {
    render(<LocalDrafts accountId="a" disabled={false} initialGmailAccountId="a" />);
    await screen.findByRole("button", { name: "Import draft: Web draft" });
    expect(mocks.listRemote).toHaveBeenCalledWith("a", "");
    expect(mocks.list).not.toHaveBeenCalled();
  });
  it("does not apply a Drafts-folder request to the wrong account", async () => {
    render(<LocalDrafts accountId="b" disabled={false} initialGmailAccountId="a" />);
    await act(async () => {});
    expect(mocks.listRemote).not.toHaveBeenCalled();
    expect(mocks.open).not.toHaveBeenCalled();
  });
});
