import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  summary: vi.fn(),
  list: vi.fn(),
  dismiss: vi.fn(),
  invalidate: vi.fn(),
  listener: undefined as undefined | (() => void),
}));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }),
}));
vi.mock("@tauri-apps/api/event", () => ({
  listen: vi.fn((_name: string, callback: () => void) => {
    mocks.listener = callback;
    return Promise.resolve(vi.fn());
  }),
}));
vi.mock("@/lib/api", () => ({
  dismissFailedPendingMailOps: mocks.dismiss,
}));
vi.mock("@/hooks/queries", () => ({
  useAccountsQuery: () => ({ data: [{ id: "a", email: "a@example.invalid" }] }),
  usePendingMailOpsSummary: () => ({ data: mocks.summary(), isLoading: false, isFetching: false, refetch: vi.fn() }),
  usePendingMailOpsQuery: () => ({ data: mocks.list(), isLoading: false, isFetching: false, refetch: vi.fn() }),
  pendingMailOpsQueryKey: (id: string | null) => ["pendingMailOpsList", id],
  pendingMailOpsSummaryQueryKey: (id: string | null) => ["pendingMailOps", id],
}));
vi.mock("@/stores/mail.store", () => ({
  useMailStore: (selector: (state: { activeAccountId: string | null }) => unknown) => selector({ activeAccountId: null }),
}));

import PendingOpsTab from "@/features/settings/PendingOpsTab";

function renderTab() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}><PendingOpsTab /></QueryClientProvider>);
}

const emptySummary = { pending_count: 0, in_progress_count: 0, failed_count: 0, total_active_count: 0, last_error: null, updated_at: null };

beforeEach(() => {
  vi.clearAllMocks();
  mocks.summary.mockReturnValue(emptySummary);
  mocks.list.mockReturnValue([]);
  mocks.dismiss.mockResolvedValue(1);
});

describe("PendingOpsTab durable queue", () => {
  it("renders the real queue, retry time and error", () => {
    mocks.summary.mockReturnValue({ ...emptySummary, failed_count: 1, total_active_count: 1, last_error: "Gmail network request failed" });
    mocks.list.mockReturnValue([{ id: "op-1", account_id: "a", message_id: "a:m", op_type: "archive", status: "failed", attempts: 2, last_error: "Gmail network request failed", created_at: 1, updated_at: 2, next_retry_at: 3 }]);
    renderTab();
    expect(screen.getByText("Archive")).toBeTruthy();
    expect(screen.getByText("Next retry")).toBeTruthy();
    expect(screen.getAllByText("Gmail network request failed").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Failed").length).toBeGreaterThan(0);
  });

  it("shows an empty queue without failed controls", () => {
    renderTab();
    expect(screen.getByText("No pending remote writes.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Dismiss failed" })).toBeNull();
  });

  it("dismisses failed writes and reports a failure without hiding the queue", async () => {
    mocks.summary.mockReturnValue({ ...emptySummary, failed_count: 1, total_active_count: 1 });
    mocks.list.mockReturnValue([{ id: "op-1", account_id: "a", message_id: "a:m", op_type: "update_flags", status: "failed", attempts: 1, last_error: null, created_at: 1, updated_at: 2, next_retry_at: null }]);
    renderTab();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss failed" }));
    await waitFor(() => expect(mocks.dismiss).toHaveBeenCalledWith(null));
    mocks.dismiss.mockRejectedValueOnce(new Error("private backend error"));
    fireEvent.click(screen.getByRole("button", { name: "Dismiss failed" }));
    expect((await screen.findByRole("alert")).textContent).toContain("Could not dismiss failed writes");
    expect(screen.queryByText("private backend error")).toBeNull();
  });
});
