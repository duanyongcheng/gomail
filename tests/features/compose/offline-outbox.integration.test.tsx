import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, onlineManager } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import OutboxPanel from "@/features/compose/OutboxPanel";
import LocalDrafts from "@/features/compose/LocalDrafts";
import { useSendEmailMutation } from "@/hooks/mutations/useSendEmailMutation";
import { useComposeDraft } from "@/hooks/useComposeDraft";
import type { Account, DraftSummary, OutboxActionRequest, OutboxItem, SaveDraftRequest, SendRequest } from "@/mygo";
import i18n from "@/lib/i18n";

const { native, listeners } = vi.hoisted(() => ({
  native: vi.fn<(method: string, args: unknown) => Promise<unknown>>(),
  listeners: new Map<string, Set<(payload: unknown) => void>>(),
}));
vi.mock("mygo-runtime", () => ({
  call: native, event: vi.fn(),
  on: (name: string, listener: (payload: unknown) => void) => {
    const callbacks = listeners.get(name) ?? new Set(); callbacks.add(listener); listeners.set(name, callbacks);
    return () => callbacks.delete(listener);
  },
}));

const accounts: Account[] = ["a", "b"].map(id => ({ id, email: `${id}@example.invalid`, display_name: id, provider: "gmail", color: null, account_label: null, provider_display_name: null, created_at: 1, updated_at: 1, sort_order: 0, capabilities: { read: true, modify_flags: true, send: true, html: true } }));
const item = (id: string, accountId = "a", status = "queued"): OutboxItem => ({ id, accountId, status, subject: id, attempts: 0, nextRetryAt: null, lastError: "", createdAt: 1 });
let client: QueryClient;
let rows: OutboxItem[];
let drafts: DraftSummary[];
let queueFailure: boolean;
const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
const emit = (name: string) => listeners.get(name)?.forEach(listener => listener({}));
const methods = () => native.mock.calls.map(([method]) => method);

beforeEach(async () => {
  await i18n.changeLanguage("en");
  native.mockReset(); listeners.clear(); rows = []; drafts = []; queueFailure = false;
  // No networkMode override, no preloaded cache, real onlineManager offline.
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  native.mockImplementation(async (method, args) => {
    switch (method) {
      case "AccountService.List": return accounts;
      case "OutboxService.List": {
        const { accountId } = args as { accountId: string };
        return rows.filter(row => !accountId || row.accountId === accountId).map(row => ({ ...row }));
      }
      case "OutboxService.Queue": {
        if (queueFailure) throw new Error("Synthetic local queue failure");
        const request = args as SendRequest;
        rows.push({ ...item("Frozen offline send", request.accountId), subject: request.subject });
        emit("mail:outbox-changed");
        return {};
      }
      case "OutboxService.Action": {
        const request = args as OutboxActionRequest;
        expect(request.action).toBe("check");
        throw new Error("Synthetic inconclusive check");
      }
      case "ComposeService.List": return drafts;
      case "ComposeService.Save": {
        const request = args as SaveDraftRequest;
        drafts = [{ id: "saved-offline", accountId: request.accountId, subject: request.subject, updatedAt: 1, attachmentCount: request.attachmentPaths.length, cloudState: "local", cloudAvailable: true }];
        return "saved-offline";
      }
      default: throw new Error(`Unexpected synthetic transport: ${method}`);
    }
  });
  onlineManager.setOnline(false);
});
afterEach(() => {
  cleanup(); client.clear(); listeners.clear(); onlineManager.setOnline(true); vi.useRealTimers();
});

it("reads an uncached outbox, filters accounts, refreshes and handles native invalidation offline", async () => {
  rows = [item("A queued"), item("B queued", "b")];
  render(<OutboxPanel />, { wrapper });
  fireEvent.click(screen.getByText(/^Outbox/));
  await screen.findByText("A queued"); await screen.findByText("B queued");
  await waitFor(() => expect(screen.getByRole("combobox").querySelectorAll("option")).toHaveLength(3));
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "b" } });
  await waitFor(() => expect(screen.queryByText("A queued")).not.toBeInTheDocument());
  await screen.findByText("B queued");
  expect(native).toHaveBeenCalledWith("OutboxService.List", { accountId: "b" });
  rows.push(item("B refreshed", "b"));
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  await screen.findByText("B refreshed");
  rows.push(item("B event", "b"));
  await act(async () => { emit("mail:outbox-changed"); });
  await screen.findByText("B event");
  expect(onlineManager.isOnline()).toBe(false);
  expect(methods().every(method => ["AccountService.List", "OutboxService.List"].includes(method))).toBe(true);
});

it("continues the local outbox polling interval while browser connectivity stays offline", async () => {
  rows = [item("Initial poll")];
  vi.useFakeTimers();
  render(<OutboxPanel accountId="a" />, { wrapper });
  await act(async () => { await vi.advanceTimersByTimeAsync(0); });
  expect(native).toHaveBeenCalledWith("OutboxService.List", { accountId: "a" });
  const reads = methods().filter(method => method === "OutboxService.List").length;
  rows.push(item("Polled offline"));
  await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
  expect(methods().filter(method => method === "OutboxService.List")).toHaveLength(reads + 1);
  expect(client.getQueryData<OutboxItem[]>(["outbox", "a"])?.map(row => row.subject)).toEqual(["Initial poll", "Polled offline"]);
  expect(methods()).not.toContain("OutboxService.Action");
  expect(onlineManager.isOnline()).toBe(false);
});

it("queues an actual local send mutation offline and refreshes Outbox without direct network submission", async () => {
  render(<OutboxPanel accountId="a" />, { wrapper });
  fireEvent.click(screen.getByText(/^Outbox/));
  const { result } = renderHook(useSendEmailMutation, { wrapper });
  const params = { accountId: "a", to: ["to@example.invalid"], cc: [], bcc: ["bcc@example.invalid"], subject: "Queued offline", bodyText: "Immutable synthetic content", clientKey: "offline-compose", localDraftId: "local-a", attachmentPaths: ["compose-attachment:synthetic"] };
  // Do not await a paused mutateAsync until the local binding has actually run.
  act(() => { result.current.mutate(params); });
  await waitFor(() => expect(native).toHaveBeenCalledWith("OutboxService.Queue", expect.objectContaining(params)));
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  await screen.findByText("Queued offline");
  expect(rows).toHaveLength(1); expect(rows[0].status).toBe("queued");
  expect(methods().filter(method => method === "OutboxService.Queue")).toHaveLength(1);
  expect(methods().every(method => ["AccountService.List", "OutboxService.List", "OutboxService.Queue"].includes(method))).toBe(true);
  expect(onlineManager.isOnline()).toBe(false);
});

it("surfaces local enqueue failure once without automatic retry or cloud submission", async () => {
  queueFailure = true;
  const { result } = renderHook(useSendEmailMutation, { wrapper });
  act(() => { result.current.mutate({ accountId: "a", to: ["to@example.invalid"], cc: [], bcc: [], subject: "Keep draft", bodyText: "body" }); });
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.error?.message).toBe("Synthetic local queue failure");
  expect(rows).toEqual([]); expect(methods()).toEqual(["OutboxService.Queue"]);
});

it("keeps local draft autosave/list available offline and never auto-uploads or opens Gmail drafts", async () => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  const { result, rerender } = renderHook(({ subject }) => useComposeDraft({
    to: ["to@example.invalid"], cc: [], bcc: [], subject, rawSource: "", richTextHtml: "<p>Saved locally</p>",
    editorMode: "rich", composeMode: "new", fromAccountId: "a", attachments: [], editorReady: true,
  }), { wrapper, initialProps: { subject: "Initial local" } });
  rerender({ subject: "Offline autosave" });
  await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
  expect(native).toHaveBeenCalledWith("ComposeService.Save", expect.objectContaining({ subject: "Offline autosave", accountId: "a" }));
  expect(result.current.saveStatus).toBe("saved");
  vi.useRealTimers();
  render(<LocalDrafts accountId="a" disabled={false} />, { wrapper });
  fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
  await screen.findByText("Offline autosave");
  expect(methods().every(method => ["ComposeService.Save", "ComposeService.List"].includes(method))).toBe(true);
  expect(onlineManager.isOnline()).toBe(false);
});

it("does not automatically run or retry Check Sent for unknown cloud-bound outcomes on reads or reconnect", async () => {
  rows = [{ ...item("Unknown cloud send", "a", "outcome_unknown"), cloudDraft: true }];
  render(<OutboxPanel accountId="a" />, { wrapper });
  fireEvent.click(screen.getByText(/^Outbox/));
  const check = await screen.findByRole("button", { name: "Check Sent" });
  expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Dismiss" })).not.toBeInTheDocument();
  expect(methods()).not.toContain("OutboxService.Action");
  fireEvent.click(check);
  await screen.findByText(/Sent check is inconclusive. No resend was attempted/);
  await act(async () => { await client.invalidateQueries({ queryKey: ["outbox"] }); onlineManager.setOnline(true); });
  expect(methods().filter(method => method === "OutboxService.Action")).toHaveLength(1);
  expect(methods()).not.toContain("OutboxService.Queue");
  expect(methods().some(method => method.startsWith("ComposeService.Cloud"))).toBe(false);
});
