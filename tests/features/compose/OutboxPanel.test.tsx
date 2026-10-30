import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
const mocks = vi.hoisted(() => ({ list: vi.fn(), action: vi.fn(), confirm: vi.fn() }));
vi.mock("@/hooks/queries", () => ({ useAccountsQuery: () => ({ data: [{ id: "a", email: "a@example.invalid" }] }) }));
vi.mock("@tauri-apps/api/event", () => ({ listen: () => Promise.resolve(() => {}) }));
vi.mock("@/mygo", () => ({ OutboxService: { list: mocks.list, action: mocks.action } }));
vi.mock("@/stores/confirm.store", () => ({ useConfirmStore: { getState: () => ({ confirm: mocks.confirm }) } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (_key: string, fallback: string) => fallback }) }));
import OutboxPanel from "@/features/compose/OutboxPanel";
let client: QueryClient;
beforeEach(() => { vi.resetAllMocks(); mocks.action.mockResolvedValue({}); mocks.confirm.mockResolvedValue(true); client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); });
afterEach(() => { cleanup(); client.clear(); });
function show() { return render(<QueryClientProvider client={client}><OutboxPanel accountId="a" /></QueryClientProvider>); }
function item(status: string) { return { id: "id", accountId: "a", status, subject: "message", attempts: 1, nextRetryAt: null, lastError: "", createdAt: 1 }; }
it("offers check but never retry/cancel for unknown sends", async () => {
  mocks.list.mockResolvedValue([item("outcome_unknown")]); show();
  fireEvent.click(screen.getByText(/Outbox/));
  const check = await screen.findByRole("button", { name: "Check Sent" });
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Cancel queued send" })).toBeNull();
  fireEvent.click(check);
  await waitFor(() => expect(mocks.action).toHaveBeenCalledWith({ accountId: "a", id: "id", action: "check" }));
  expect(mocks.list).toHaveBeenCalledWith({ accountId: "a" });
});
it("supports definite failure retry and reports inconclusive checks", async () => {
  mocks.list.mockResolvedValue([item("failed")]); show(); fireEvent.click(screen.getByText(/Outbox/));
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
  await waitFor(() => expect(mocks.action).toHaveBeenCalledWith(expect.objectContaining({ action: "retry" })));
});
it("filters by account in the settings surface", async () => {
  mocks.list.mockResolvedValue([]);
  render(<QueryClientProvider client={client}><OutboxPanel /></QueryClientProvider>);
  fireEvent.click(screen.getByText(/Outbox/));
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "a" } });
  await waitFor(() => expect(mocks.list).toHaveBeenCalledWith({ accountId: "a" }));
});

it("keeps unknown state when the read-only check fails", async () => {
  mocks.list.mockResolvedValue([item("outcome_unknown")]); mocks.action.mockRejectedValue(new Error("no match"));
  show(); fireEvent.click(screen.getByText(/Outbox/));
  fireEvent.click(await screen.findByRole("button", { name: "Check Sent" }));
  expect((await screen.findByRole("alert")).textContent).toContain("No resend was attempted");
});
it("retains the reconciliation identity for an unknown cloud-draft send", async () => {
  mocks.list.mockResolvedValue([{ ...item("outcome_unknown"), cloudDraft: true }]);
  show(); fireEvent.click(screen.getByText(/Outbox/));
  const check = await screen.findByRole("button", { name: "Check Sent" });
  expect(screen.queryByRole("button", { name: "Dismiss" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  expect(screen.getByText(/linked to a Gmail draft/)).toBeTruthy();
  fireEvent.click(check);
  await waitFor(() => expect(mocks.action).toHaveBeenCalledWith({ accountId: "a", id: "id", action: "check" }));
});
it("still allows dismissal of confirmed cloud sends and ordinary unknown sends", async () => {
  mocks.list.mockResolvedValue([{ ...item("sent"), cloudDraft: true }, { ...item("outcome_unknown"), id: "ordinary" }]);
  show(); fireEvent.click(screen.getByText(/Outbox/));
  expect(await screen.findAllByRole("button", { name: "Dismiss" })).toHaveLength(2);
});
