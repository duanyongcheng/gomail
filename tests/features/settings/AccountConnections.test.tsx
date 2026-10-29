import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Account } from "@/lib/api";
import type { AccountConnection } from "@/mygo";

const mocks = vi.hoisted(() => ({ connections: vi.fn(), reconnect: vi.fn(), cancel: vi.fn(), listAccounts: vi.fn(), changed: null as null | (() => void) }));
vi.mock("@/mygo", () => ({ AccountService: { connections: mocks.connections, reconnect: mocks.reconnect, cancelReconnect: mocks.cancel } }));
vi.mock("@/lib/api", () => ({ listAccounts: mocks.listAccounts }));
vi.mock("@tauri-apps/api/event", () => ({ listen: (_name: string, callback: () => void) => { mocks.changed = callback; return Promise.resolve(() => {}); } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (_key: string, fallback: string) => fallback }) }));
import AccountConnections from "@/features/settings/AccountConnections";
import { useAccountsQuery } from "@/hooks/queries/useAccountsQuery";

const accounts: Account[] = ["a", "b"].map((id) => ({ id, email: `${id}@example.invalid`, display_name: id, provider: "gmail", created_at: 1, updated_at: 1 }));
const states = (): AccountConnection[] => accounts.map((a) => ({ accountId: a.id, state: "available", canReconnect: true }));
let client: QueryClient;
function show(items = accounts) { return render(<QueryClientProvider client={client}><AccountConnections accounts={items} /></QueryClientProvider>); }
beforeEach(() => {
  vi.resetAllMocks();
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mocks.connections.mockResolvedValue(states()); mocks.reconnect.mockResolvedValue({}); mocks.cancel.mockResolvedValue({}); mocks.listAccounts.mockResolvedValue(accounts);
});
afterEach(() => { cleanup(); client.clear(); });

describe("Gmail connection settings", () => {
  it("reads local status without opening sign-in and explains the mailbox boundary", async () => {
    show();
    await screen.findAllByText("Local credentials available");
    expect(mocks.connections).toHaveBeenCalledWith({});
    expect(mocks.reconnect).not.toHaveBeenCalled();
    expect(screen.getByText(/same mailbox/).textContent).toContain("Unknown sends are never resent");
  });
  it("shows account-specific authorization problems and disables unconfigured sign-in", async () => {
    mocks.connections.mockResolvedValue([
      { accountId: "a", state: "permission_required", canReconnect: true },
      { accountId: "b", state: "configuration_required", canReconnect: false },
    ]);
    show();
    await screen.findByText("Mail access permission is missing; reconnect and grant access");
    expect((screen.getByRole("button", { name: "Reconnect: a@example.invalid" }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Reconnect: b@example.invalid" }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("Configure Google OAuth before reconnecting.")).toBeTruthy();
  });
  it("guards duplicate clicks and keeps the account identity on the backend request", async () => {
    let finish!: (value: object) => void;
    mocks.reconnect.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    show(); await screen.findAllByText("Local credentials available");
    const button = screen.getByRole("button", { name: "Reconnect: a@example.invalid" });
    fireEvent.click(button); fireEvent.click(button);
    expect(mocks.reconnect).toHaveBeenCalledTimes(1);
    expect(mocks.reconnect).toHaveBeenCalledWith({ accountId: "a" });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/Waiting for Google sign-in/)).toBeTruthy();
    await act(async () => finish({}));
    await screen.findByText("Account reconnected. Your existing mail and drafts are kept.");
    await waitFor(() => expect((button as HTMLButtonElement).disabled).toBe(false));
  });
  it("can cancel sign-in while the reconnect call is still pending", async () => {
    let fail!: (err: Error) => void;
    mocks.reconnect.mockImplementation(() => new Promise((_resolve, reject) => { fail = reject; }));
    mocks.cancel.mockImplementation(async () => { fail(new Error("Google reconnection cancelled; saved credentials are unchanged.")); return {}; });
    show(); await screen.findAllByText("Local credentials available");
    fireEvent.click(screen.getByRole("button", { name: "Reconnect: a@example.invalid" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel sign-in: a@example.invalid" }));
    await waitFor(() => expect(mocks.cancel).toHaveBeenCalledWith({ accountId: "a" }));
    expect((await screen.findByRole("alert")).textContent).toContain("cancelled");
    await waitFor(() => expect(screen.queryByRole("button", { name: "Cancel sign-in: a@example.invalid" })).toBeNull());
    expect(screen.queryByText("Account reconnected. Your existing mail and drafts are kept.")).toBeNull();
  });
  it("shows a failed reconnect and retains the other account's status", async () => {
    mocks.reconnect.mockRejectedValueOnce(new Error("The selected Google mailbox does not match this account"));
    show(); await screen.findAllByText("Local credentials available");
    fireEvent.click(screen.getByRole("button", { name: "Reconnect: a@example.invalid" }));
    expect((await screen.findByRole("alert")).textContent).toContain("does not match");
    expect(screen.queryByText("Account reconnected. Your existing mail and drafts are kept.")).toBeNull();
    expect(screen.getAllByText("Local credentials available")).toHaveLength(2);
  });
  it("ignores a late response for an account removed while signing in", async () => {
    let fail!: (err: Error) => void;
    mocks.reconnect.mockImplementation(() => new Promise((_resolve, reject) => { fail = reject; }));
    const view = show(); await screen.findAllByText("Local credentials available");
    fireEvent.click(screen.getByRole("button", { name: "Reconnect: a@example.invalid" }));
    view.rerender(<QueryClientProvider client={client}><AccountConnections accounts={[accounts[1]]} /></QueryClientProvider>);
    await act(async () => fail(new Error("Stale account error")));
    await screen.findByRole("button", { name: "Reconnect: b@example.invalid" });
    expect(screen.queryByText("Stale account error")).toBeNull();
    expect(screen.queryByRole("button", { name: "Reconnect: a@example.invalid" })).toBeNull();
  });
  it("exposes read failures with an explicit retry", async () => {
    mocks.connections.mockRejectedValueOnce(new Error("storage"));
    show(); expect((await screen.findByRole("alert")).textContent).toContain("Could not read");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findAllByText("Local credentials available");
  });
  it("refreshes connection state and account capabilities after the backend event", async () => {
    function WithAccountEvents() { useAccountsQuery(); return <AccountConnections accounts={accounts} />; }
    render(<QueryClientProvider client={client}><WithAccountEvents /></QueryClientProvider>);
    await screen.findAllByText("Local credentials available");
    const calls = mocks.listAccounts.mock.calls.length;
    mocks.connections.mockResolvedValue([{ accountId: "a", state: "reauth_required", canReconnect: true }, states()[1]]);
    act(() => mocks.changed?.());
    await screen.findByText("Authorization expired or revoked; reconnect this account");
    await waitFor(() => expect(mocks.listAccounts.mock.calls.length).toBeGreaterThan(calls));
  });
});
