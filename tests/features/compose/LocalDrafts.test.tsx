import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const mocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), remove: vi.fn(), open: vi.fn(), confirm: vi.fn() }));
vi.mock("@/lib/platform/compose", () => ({ listLocalDrafts: mocks.list, getLocalDraft: mocks.get }));
vi.mock("@/lib/api", () => ({ deleteDraft: mocks.remove }));
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
});
describe("local drafts", () => {
  it("restores only after fetching the account-scoped complete draft", async () => {
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    fireEvent.click(await screen.findByRole("button", { name: "Saved draft" }));
    await waitFor(() => expect(mocks.open).toHaveBeenCalledWith("new", null, { localDraft: { id: "d", accountId: "a", subject: "Saved draft" } }));
    expect(mocks.get).toHaveBeenCalledWith("a", "d");
    expect(screen.getByText(/not synced to Gmail/)).toBeTruthy();
  });
  it("does not replace a dirty compose and deletes with confirmation", async () => {
    const view = render(<LocalDrafts accountId="a" disabled />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    expect((await screen.findByRole("button", { name: "Saved draft" }) as HTMLButtonElement).disabled).toBe(true);
    view.rerender(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Delete: Saved draft" }));
    await waitFor(() => expect(mocks.remove).toHaveBeenCalledWith("a", "d"));
  });
  it("shows read errors and supports retry", async () => {
    mocks.list.mockRejectedValueOnce(new Error("locked"));
    render(<LocalDrafts accountId="a" disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
    expect(await screen.findByRole("alert")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("button", { name: "Saved draft" })).toBeTruthy();
  });
});
